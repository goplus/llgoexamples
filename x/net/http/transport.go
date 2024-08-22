package http

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/libuv"
	"github.com/goplus/llgo/c/net"
	"github.com/goplus/llgo/c/syscall"
	xnet "github.com/goplus/llgo/x/net"
	"github.com/goplus/llgoexamples/rust/hyper"
)

// DefaultTransport is the default implementation of Transport and is
// used by DefaultClient. It establishes network connections as needed
// and caches them for reuse by subsequent calls. It uses HTTP proxies
// as directed by the environment variables HTTP_PROXY, HTTPS_PROXY
// and NO_PROXY (or the lowercase versions thereof).
var DefaultTransport RoundTripper = &Transport{
	//Proxy: ProxyFromEnvironment,
	Proxy: nil,
}

// DefaultMaxIdleConnsPerHost is the default value of Transport's
// MaxIdleConnsPerHost.
const DefaultMaxIdleConnsPerHost = 2
const defaultHTTPPort = "80"

type Transport struct {
	altProto    atomic.Value // of nil or map[string]RoundTripper, key is URI scheme
	reqMu       sync.Mutex
	reqCanceler map[cancelKey]func(error)
	Proxy       func(*Request) (*url.URL, error)

	connsPerHostMu   sync.Mutex
	connsPerHost     map[connectMethodKey]int
	connsPerHostWait map[connectMethodKey]wantConnQueue // waiting getConns

	// DisableKeepAlives, if true, disables HTTP keep-alives and
	// will only use the connection to the server for a single
	// HTTP request.
	//
	// This is unrelated to the similarly named TCP keep-alives.
	DisableKeepAlives bool

	// DisableCompression, if true, prevents the Transport from
	// requesting compression with an "Accept-Encoding: gzip"
	// request header when the Request contains no existing
	// Accept-Encoding value. If the Transport requests gzip on
	// its own and gets a gzipped response, it's transparently
	// decoded in the Response.Body. However, if the user
	// explicitly requested gzip it is not automatically
	// uncompressed.
	DisableCompression bool

	// MaxConnsPerHost optionally limits the total number of
	// connections per host, including connections in the dialing,
	// active, and idle states. On limit violation, dials will block.
	//
	// Zero means no limit.
	MaxConnsPerHost int
}

// A cancelKey is the key of the reqCanceler map.
// We wrap the *Request in this type since we want to use the original request,
// not any transient one created by roundTrip.
type cancelKey struct {
	req *Request
}

// incomparable is a zero-width, non-comparable type. Adding it to a struct
// makes that struct also non-comparable, and generally doesn't add
// any size (as long as it's first).
type incomparable [0]func()

type requestAndChan struct {
	_         incomparable
	req       *Request
	cancelKey cancelKey
	ch        chan responseAndError // unbuffered; always send in select on callerGone

	// whether the Transport (as opposed to the user client code)
	// added the Accept-Encoding gzip header. If the Transport
	// set it, only then do we transparently decode the gzip.
	addedGzip bool

	callerGone <-chan struct{} // closed when roundTrip caller has returned
}

// A writeRequest is sent by the caller's goroutine to the
// writeLoop's goroutine to write a request while the read loop
// concurrently waits on both the write response and the server's
// reply.
type writeRequest struct {
	req *transportRequest
	ch  chan<- error
}

// responseAndError is how the goroutine reading from an HTTP/1 server
// communicates with the goroutine doing the RoundTrip.
type responseAndError struct {
	_   incomparable
	res *Response // else use this response (see res method)
	err error
}

type connAndTimeoutChan struct {
	_         incomparable
	conn      *connData
	timeoutch chan struct{}
}

type readTrackingBody struct {
	io.ReadCloser
	didRead  bool
	didClose bool
}

func (r *readTrackingBody) Read(data []byte) (int, error) {
	r.didRead = true
	return r.ReadCloser.Read(data)
}

func (r *readTrackingBody) Close() error {
	r.didClose = true
	return r.ReadCloser.Close()
}

// setupRewindBody returns a new request with a custom body wrapper
// that can report whether the body needs rewinding.
// This lets rewindBody avoid an error result when the request
// does not have GetBody but the body hasn't been read at all yet.
func setupRewindBody(req *Request) *Request {
	if req.Body == nil || req.Body == NoBody {
		return req
	}
	newReq := *req
	newReq.Body = &readTrackingBody{ReadCloser: req.Body}
	return &newReq
}

// rewindBody returns a new request with the body rewound.
// It returns req unmodified if the body does not need rewinding.
// rewindBody takes care of closing req.Body when appropriate
// (in all cases except when rewindBody returns req unmodified).
func rewindBody(req *Request) (rewound *Request, err error) {
	if req.Body == nil || req.Body == NoBody || (!req.Body.(*readTrackingBody).didRead && !req.Body.(*readTrackingBody).didClose) {
		return req, nil // nothing to rewind
	}
	if !req.Body.(*readTrackingBody).didClose {
		req.closeBody()
	}
	if req.GetBody == nil {
		return nil, errCannotRewind
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	newReq := *req
	newReq.Body = &readTrackingBody{ReadCloser: body}
	return &newReq, nil
}

// transportRequest is a wrapper around a *Request that adds
// optional extra headers to write and stores any error to return
// from roundTrip.
type transportRequest struct {
	*Request        // original request, not to be mutated
	extra    Header // extra headers to write, or nil
	//trace     *httptrace.ClientTrace // optional
	cancelKey cancelKey

	mu  sync.Mutex // guards err
	err error      // first setError value for mapRoundTripError to consider
}

func (tr *transportRequest) extraHeaders() Header {
	if tr.extra == nil {
		tr.extra = make(Header)
	}
	return tr.extra
}

func (tr *transportRequest) setError(err error) {
	tr.mu.Lock()
	if tr.err == nil {
		tr.err = err
	}
	tr.mu.Unlock()
}

func (t *Transport) connectMethodForRequest(treq *transportRequest) (cm connectMethod, err error) {
	cm.targetScheme = treq.URL.Scheme
	cm.targetAddr = canonicalAddr(treq.URL)
	if t.Proxy != nil {
		cm.proxyURL, err = t.Proxy(treq.Request)
	}
	cm.treq = treq
	cm.onlyH1 = treq.requiresHTTP1()
	return cm, err
}

func (t *Transport) setReqCanceler(key cancelKey, fn func(error)) {
	t.reqMu.Lock()
	defer t.reqMu.Unlock()
	if t.reqCanceler == nil {
		t.reqCanceler = make(map[cancelKey]func(error))
	}
	if fn != nil {
		t.reqCanceler[key] = fn
	} else {
		delete(t.reqCanceler, key)
	}
}

// replaceReqCanceler replaces an existing cancel function. If there is no cancel function
// for the request, we don't set the function and return false.
// Since CancelRequest will clear the canceler, we can use the return value to detect if
// the request was canceled since the last setReqCancel call.
func (t *Transport) replaceReqCanceler(key cancelKey, fn func(error)) bool {
	t.reqMu.Lock()
	defer t.reqMu.Unlock()
	_, ok := t.reqCanceler[key]
	if !ok {
		return false
	}
	if fn != nil {
		t.reqCanceler[key] = fn
	} else {
		delete(t.reqCanceler, key)
	}
	return true
}

// alternateRoundTripper returns the alternate RoundTripper to use
// for this request if the Request's URL scheme requires one,
// or nil for the normal case of using the Transport.
func (t *Transport) alternateRoundTripper(req *Request) RoundTripper {
	if !t.useRegisteredProtocol(req) {
		return nil
	}
	altProto, _ := t.altProto.Load().(map[string]RoundTripper)
	return altProto[req.URL.Scheme]
}

// useRegisteredProtocol reports whether an alternate protocol (as registered
// with Transport.RegisterProtocol) should be respected for this request.
func (t *Transport) useRegisteredProtocol(req *Request) bool {
	if req.URL.Scheme == "https" && req.requiresHTTP1() {
		// If this request requires HTTP/1, don't use the
		// "https" alternate protocol, which is used by the
		// HTTP/2 code to take over requests if there's an
		// existing cached HTTP/2 connection.
		return false
	}
	return true
}

// CancelRequest cancels an in-flight request by closing its connection.
// CancelRequest should only be called after RoundTrip has returned.
//
// Deprecated: Use Request.WithContext to create a request with a
// cancelable context instead. CancelRequest cannot cancel HTTP/2
// requests.
func (t *Transport) CancelRequest(req *Request) {
	t.cancelRequest(cancelKey{req}, errRequestCanceled)
}

// Cancel an in-flight request, recording the error value.
// Returns whether the request was canceled.
func (t *Transport) cancelRequest(key cancelKey, err error) bool {
	// This function must not return until the cancel func has completed.
	// See: https://golang.org/issue/34658
	t.reqMu.Lock()
	defer t.reqMu.Unlock()
	cancel := t.reqCanceler[key]
	delete(t.reqCanceler, key)
	if cancel != nil {
		cancel(err)
	}

	return cancel != nil
}

// ----------------------------------------------------------

func (t *Transport) RoundTrip(req *Request) (*Response, error) {
	//t.nextProtoOnce.Do(t.onceSetNextProtoDefaults)
	//ctx := req.Context()
	//trace := httptrace.ContextClientTrace(ctx)

	if req.URL == nil {
		req.closeBody()
		return nil, errors.New("http: nil Request.URL")
	}
	if req.Header == nil {
		req.closeBody()
		return nil, errors.New("http: nil Request.Header")
	}
	scheme := req.URL.Scheme
	isHTTP := scheme == "http" || scheme == "https"
	if isHTTP {
		for k, vv := range req.Header {
			if !ValidHeaderFieldName(k) {
				req.closeBody()
				return nil, fmt.Errorf("net/http: invalid header field name %q", k)
			}
			for _, v := range vv {
				if !ValidHeaderFieldValue(v) {
					req.closeBody()
					// Don't include the value in the error, because it may be sensitive.
					return nil, fmt.Errorf("net/http: invalid header field value for %q", k)
				}
			}
		}
	}

	origReq := req
	cancelKey := cancelKey{origReq}
	req = setupRewindBody(req)

	if altRT := t.alternateRoundTripper(req); altRT != nil {
		if resp, err := altRT.RoundTrip(req); err != ErrSkipAltProtocol {
			return resp, err
		}
		var err error
		req, err = rewindBody(req)
		if err != nil {
			return nil, err
		}
	}
	if !isHTTP {
		req.closeBody()
		return nil, badStringError("unsupported protocol scheme", scheme)
	}
	if req.Method != "" && !validMethod(req.Method) {
		req.closeBody()
		return nil, fmt.Errorf("net/http: invalid method %q", req.Method)
	}
	if req.URL.Host == "" {
		req.closeBody()
		return nil, errors.New("http: no Host in request URL")
	}

	for {
		// TODO(spongehah) timeout: because of that ctx not initialized ( initialized in setRequestCancel() )
		//select {
		//case <-ctx.Done():
		//	req.closeBody()
		//	return nil, ctx.Err()
		//default:
		//}

		// treq gets modified by roundTrip, so we need to recreate for each retry.
		//treq := &transportRequest{Request: req, trace: trace, cancelKey: cancelKey}
		treq := &transportRequest{Request: req, cancelKey: cancelKey}
		cm, err := t.connectMethodForRequest(treq)
		if err != nil {
			req.closeBody()
			return nil, err
		}

		// Get the cached or newly-created connection to either the
		// host (for http or https), the http proxy, or the http proxy
		// pre-CONNECTed to https server. In any case, we'll be ready
		// to send it requests.
		pconn, err := t.getConn(treq, cm)
		if err != nil {
			t.setReqCanceler(cancelKey, nil)
			req.closeBody()
			return nil, err
		}

		var resp *Response
		if pconn.alt != nil {
			// HTTP/2 path.
			t.setReqCanceler(cancelKey, nil) // not cancelable with CancelRequest
			resp, err = pconn.alt.RoundTrip(req)
		} else {
			resp, err = pconn.roundTrip(treq)
		}
		if err == nil {
			resp.Request = origReq
			return resp, nil
		}

		// Failed. Clean up and determine whether to retry.
		// TODO(spongehah) Retry & ConnPool
		return nil, err
	}
}

func (t *Transport) getConn(treq *transportRequest, cm connectMethod) (pc *persistConn, err error) {
	//req := treq.Request
	//trace := treq.trace
	//ctx := req.Context()
	//if trace != nil && trace.GetConn != nil {
	//	trace.GetConn(cm.addr())
	//}

	w := &wantConn{
		cm:  cm,
		key: cm.key(),
		//ctx:        ctx,
		ready:      make(chan struct{}, 1),
		beforeDial: testHookPrePendingDial,
		afterDial:  testHookPostPendingDial,
	}
	defer func() {
		if err != nil {
			w.cancel(t, err)
		}
	}()

	// TODO(spongehah) ConnPool
	//// Queue for idle connection.
	//if delivered := t.queueForIdleConn(w); delivered {
	//	pc := w.pc
	//	// Trace only for HTTP/1.
	//	// HTTP/2 calls trace.GotConn itself.
	//	if pc.alt == nil && trace != nil && trace.GotConn != nil {
	//		trace.GotConn(pc.gotIdleConnTrace(pc.idleAt))
	//	}
	//	// set request canceler to some non-nil function so we
	//	// can detect whether it was cleared between now and when
	//	// we enter roundTrip
	//	t.setReqCanceler(treq.cancelKey, func(error) {})
	//	return pc, nil
	//}

	cancelc := make(chan error, 1)
	t.setReqCanceler(treq.cancelKey, func(err error) { cancelc <- err })

	// Queue for permission to dial.
	t.queueForDial(w)

	// Wait for completion or cancellation.
	select {
	case <-w.ready:
		// Trace success but only for HTTP/1.
		// HTTP/2 calls trace.GotConn itself.
		//if w.pc != nil && w.pc.alt == nil && trace != nil && trace.GotConn != nil {
		//	trace.GotConn(httptrace.GotConnInfo{Conn: w.pc.conn, Reused: w.pc.isReused()})
		//}
		if w.err != nil {
			// If the request has been canceled, that's probably
			// what caused w.err; if so, prefer to return the
			// cancellation error (see golang.org/issue/16049).
			select {
			//case <-req.Cancel:
			//	return nil, errRequestCanceledConn
			//case <-req.Context().Done():
			//	return nil, req.Context().Err()
			case err := <-cancelc:
				if err == errRequestCanceled {
					err = errRequestCanceledConn
				}
				return nil, err
			default:
				// return below
			}
		}
		return w.pc, w.err
	// TODO(spongehah) cancel(t.getConn)
	//case <-req.Cancel:
	//	return nil, errRequestCanceledConn
	case <-treq.Request.timeoutch:
		return nil, fmt.Errorf("request timeout\n")
	//case <-req.Context().Done():
	//	return nil, req.Context().Err()
	case err := <-cancelc:
		if err == errRequestCanceled {
			err = errRequestCanceledConn
		}
		return nil, err
	}
}

// queueForDial queues w to wait for permission to begin dialing.
// Once w receives permission to dial, it will do so in a separate goroutine.
func (t *Transport) queueForDial(w *wantConn) {
	w.beforeDial()

	if t.MaxConnsPerHost <= 0 {
		go t.dialConnFor(w)
		return
	}

	t.connsPerHostMu.Lock()
	defer t.connsPerHostMu.Unlock()

	if n := t.connsPerHost[w.key]; n < t.MaxConnsPerHost {
		if t.connsPerHost == nil {
			t.connsPerHost = make(map[connectMethodKey]int)
		}
		t.connsPerHost[w.key] = n + 1
		go t.dialConnFor(w)
		return
	}

	if t.connsPerHostWait == nil {
		t.connsPerHostWait = make(map[connectMethodKey]wantConnQueue)
	}
	q := t.connsPerHostWait[w.key]
	q.cleanFront()
	q.pushBack(w)
	t.connsPerHostWait[w.key] = q
}

// dialConnFor dials on behalf of w and delivers the result to w.
// dialConnFor has received permission to dial w.cm and is counted in t.connCount[w.cm.key()].
// If the dial is canceled or unsuccessful, dialConnFor decrements t.connCount[w.cm.key()].
func (t *Transport) dialConnFor(w *wantConn) {
	defer w.afterDial()

	pc, err := t.dialConn(w.ctx, w.cm)
	w.tryDeliver(pc, err)
	// TODO(spongehah) ConnPool
	//delivered := w.tryDeliver(pc, err)
	// Handle undelivered or shareable connections
	//if err == nil && (!delivered || pc.alt != nil) {
	//	// pconn was not passed to w,
	//	// or it is HTTP/2 and can be shared.
	//	// Add to the idle connection pool.
	//	t.putOrCloseIdleConn(pc)
	//}

	// If an error occurs during the dialing process, the connection count for that host is decreased.
	// This ensures that the connection count remains accurate even in cases where the dial attempt fails.
	if err != nil {
		t.decConnsPerHost(w.key)
	}
}

// decConnsPerHost decrements the per-host connection count for key,
// which may in turn give a different waiting goroutine permission to dial.
func (t *Transport) decConnsPerHost(key connectMethodKey) {
	if t.MaxConnsPerHost <= 0 {
		return
	}

	t.connsPerHostMu.Lock()
	defer t.connsPerHostMu.Unlock()
	n := t.connsPerHost[key]
	if n == 0 {
		// Shouldn't happen, but if it does, the counting is buggy and could
		// easily lead to a silent deadlock, so report the problem loudly.
		panic("net/http: internal error: connCount underflow")
	}

	// Can we hand this count to a goroutine still waiting to dial?
	// (Some goroutines on the wait list may have timed out or
	// gotten a connection another way. If they're all gone,
	// we don't want to kick off any spurious dial operations.)
	if q := t.connsPerHostWait[key]; q.len() > 0 {
		done := false
		for q.len() > 0 {
			w := q.popFront()
			if w.waiting() {
				go t.dialConnFor(w)
				done = true
				break
			}
		}
		if q.len() == 0 {
			delete(t.connsPerHostWait, key)
		} else {
			// q is a value (like a slice), so we have to store
			// the updated q back into the map.
			t.connsPerHostWait[key] = q
		}
		if done {
			return
		}
	}

	// Otherwise, decrement the recorded count.
	if n--; n == 0 {
		delete(t.connsPerHost, key)
	} else {
		t.connsPerHost[key] = n
	}
}

func (t *Transport) dialConn(ctx context.Context, cm connectMethod) (pconn *persistConn, err error) {
	pconn = &persistConn{
		t:             t,
		cacheKey:      cm.key(),
		reqch:         make(chan requestAndChan, 1),
		writech:       make(chan writeRequest, 1),
		closech:       make(chan struct{}, 1),
		writeLoopDone: make(chan struct{}, 1),
	}

	//if cm.scheme() == "https" && t.hasCustomTLSDialer() {
	//	var err error
	//	pconn.conn, err = t.customDialTLS(ctx, "tcp", cm.addr())
	//	if err != nil {
	//		return nil, wrapErr(err)
	//	}
	//	if tc, ok := pconn.conn.(*tls.Conn); ok {
	//		// Handshake here, in case DialTLS didn't. TLSNextProto below
	//		// depends on it for knowing the connection state.
	//		if trace != nil && trace.TLSHandshakeStart != nil {
	//			trace.TLSHandshakeStart()
	//		}
	//		if err := tc.HandshakeContext(ctx); err != nil {
	//			go pconn.conn.Close()
	//			if trace != nil && trace.TLSHandshakeDone != nil {
	//				trace.TLSHandshakeDone(tls.ConnectionState{}, err)
	//			}
	//			return nil, err
	//		}
	//		cs := tc.ConnectionState()
	//		if trace != nil && trace.TLSHandshakeDone != nil {
	//			trace.TLSHandshakeDone(cs, nil)
	//		}
	//		pconn.tlsState = &cs
	//	}
	//} else {
	//conn, err := t.dial(ctx, "tcp", cm.addr())
	conn, err := t.dial(ctx, pconn, cm)
	if err != nil {
		return nil, err
	}
	pconn.conn = conn

	// hyper specific
	// Hookup the IO
	hyperIo := newIoWithConnReadWrite(conn)
	// We need an executor generally to poll futures
	exec := hyper.NewExecutor()
	// Prepare client options
	opts := hyper.NewClientConnOptions()
	opts.Exec(exec)
	pconn.io = hyperIo
	pconn.exec = exec
	pconn.opts = opts
	// send the handshake
	handshakeTask := hyper.Handshake(hyperIo, opts)
	setTaskId(handshakeTask, write)
	// Let's wait for the handshake to finish...
	exec.Push(handshakeTask)

	//if cm.scheme() == "https" {
	//	var firstTLSHost string
	//	if firstTLSHost, _, err = net.SplitHostPort(cm.addr()); err != nil {
	//		return nil, wrapErr(err)
	//	}
	//	if err = pconn.addTLS(ctx, firstTLSHost, trace); err != nil {
	//		return nil, wrapErr(err)
	//	}
	//}
	//}

	// TODO(spongehah) Proxy(https/sock5)
	// Proxy setup.
	switch {
	case cm.proxyURL == nil:
		// Do nothing. Not using a proxy.
	// case cm.proxyURL.Scheme == "socks5":
	case cm.targetScheme == "http":
		pconn.isProxy = true
		if pa := cm.proxyAuth(); pa != "" {
			pconn.mutateHeaderFunc = func(h Header) {
				h.Set("Proxy-Authorization", pa)
			}
		}
		// case cm.targetScheme == "https":
	}

	//if cm.proxyURL != nil && cm.targetScheme == "https" {
	//	if err := pconn.addTLS(ctx, cm.tlsHost(), trace); err != nil {
	//		return nil, err
	//	}
	//}
	//
	//if s := pconn.tlsState; s != nil && s.NegotiatedProtocolIsMutual && s.NegotiatedProtocol != "" {
	//	if next, ok := t.TLSNextProto[s.NegotiatedProtocol]; ok {
	//		alt := next(cm.targetAddr, pconn.conn.(*tls.Conn))
	//		if e, ok := alt.(erringRoundTripper); ok {
	//			// pconn.conn was closed by next (http2configureTransports.upgradeFn).
	//			return nil, e.RoundTripErr()
	//		}
	//		return &persistConn{t: t, cacheKey: pconn.cacheKey, alt: alt}, nil
	//	}
	//}

	go pconn.readWriteLoop(libuv.DefaultLoop())

	return pconn, nil
}

func (t *Transport) dial(ctx context.Context, pconn *persistConn, cm connectMethod) (*connData, error) {
	treq := cm.treq
	host := treq.URL.Hostname()
	port := treq.URL.Port()
	if port == "" {
		port = defaultHTTPPort
	}
	loop := libuv.DefaultLoop()
	conn := new(connData)
	if conn == nil {
		return nil, fmt.Errorf("Failed to allocate memory for conn_data\n")
	}

	// If timeout is set, start the timer
	if treq.timeout > 0 {
		libuv.InitTimer(loop, &conn.TimeoutTimer)
		ct := &connAndTimeoutChan{
			conn:      conn,
			timeoutch: treq.Request.timeoutch,
		}
		(*libuv.Handle)(c.Pointer(&conn.TimeoutTimer)).SetData(c.Pointer(ct))
		conn.TimeoutTimer.Start(onTimeout, uint64(treq.timeout.Milliseconds()), 0)
	}

	libuv.InitTcp(loop, &conn.TcpHandle)
	(*libuv.Handle)(c.Pointer(&conn.TcpHandle)).SetData(c.Pointer(conn))

	var hints net.AddrInfo
	c.Memset(c.Pointer(&hints), 0, unsafe.Sizeof(hints))
	hints.Family = syscall.AF_UNSPEC
	hints.SockType = syscall.SOCK_STREAM

	var res *net.AddrInfo
	status := net.Getaddrinfo(c.AllocaCStr(host), c.AllocaCStr(port), &hints, &res)
	if status != 0 {
		close(treq.Request.timeoutch)
		return nil, fmt.Errorf("getaddrinfo error\n")
	}

	(*libuv.Req)(c.Pointer(&conn.ConnectReq)).SetData(c.Pointer(conn))
	status = libuv.TcpConnect(&conn.ConnectReq, &conn.TcpHandle, res.Addr, onConnect)
	if status != 0 {
		close(treq.Request.timeoutch)
		return nil, fmt.Errorf("connect error: %s\n", c.GoString(libuv.Strerror(libuv.Errno(status))))
	}

	net.Freeaddrinfo(res)
	return conn, nil
}

func (pc *persistConn) roundTrip(req *transportRequest) (resp *Response, err error) {
	testHookEnterRoundTrip()
	if !pc.t.replaceReqCanceler(req.cancelKey, pc.cancelRequest) {
		// TODO(spongehah) ConnPool
		//pc.t.putOrCloseIdleConn(pc)
		return nil, errRequestCanceled
	}
	pc.mu.Lock()
	pc.numExpectedResponses++
	headerFn := pc.mutateHeaderFunc
	pc.mu.Unlock()

	if headerFn != nil {
		headerFn(req.extraHeaders())
	}

	// Ask for a compressed version if the caller didn't set their
	// own value for Accept-Encoding. We only attempt to
	// uncompress the gzip stream if we were the layer that
	// requested it.
	requestedGzip := false
	if !pc.t.DisableCompression &&
		req.Header.Get("Accept-Encoding") == "" &&
		req.Header.Get("Range") == "" &&
		req.Method != "HEAD" {
		// Request gzip only, not deflate. Deflate is ambiguous and
		// not as universally supported anyway.
		// See: https://zlib.net/zlib_faq.html#faq39
		//
		// Note that we don't request this for HEAD requests,
		// due to a bug in nginx:
		//   https://trac.nginx.org/nginx/ticket/358
		//   https://golang.org/issue/5522
		//
		// We don't request gzip if the request is for a range, since
		// auto-decoding a portion of a gzipped document will just fail
		// anyway. See https://golang.org/issue/8923
		requestedGzip = true
		req.extraHeaders().Set("Accept-Encoding", "gzip")
	}

	// The 100-continue operation in Hyper is handled in the newHyperRequest function.

	// Keep-Alive
	if pc.t.DisableKeepAlives &&
		!req.wantsClose() &&
		!isProtocolSwitchHeader(req.Header) {
		req.extraHeaders().Set("Connection", "close")
	}

	gone := make(chan struct{}, 1)
	defer close(gone)

	defer func() {
		if err != nil {
			pc.t.setReqCanceler(req.cancelKey, nil)
		}
	}()

	const debugRoundTrip = false // Debug switch provided for developers

	// Write the request concurrently with waiting for a response,
	// in case the server decides to reply before reading our full
	// request body.

	// In Hyper, the writeLoop() and readLoop() are combined together --> readWriteLoop().
	startBytesWritten := pc.nwrite
	writeErrCh := make(chan error, 1)
	pc.writech <- writeRequest{req: req, ch: writeErrCh}

	// Send the request to readWriteLoop().
	resc := make(chan responseAndError, 1)
	pc.reqch <- requestAndChan{
		req:        req.Request,
		cancelKey:  req.cancelKey,
		ch:         resc,
		addedGzip:  requestedGzip,
		callerGone: gone,
	}

	//var respHeaderTimer <-chan time.Time
	//cancelChan := req.Request.Cancel
	//ctxDoneChan := req.Context().Done()
	pcClosed := pc.closech
	canceled := false

	for {
		testHookWaitResLoop()

		select {
		case err := <-writeErrCh:
			if debugRoundTrip {
				//req.logf("writeErrCh resv: %T/%#v", err, err)
			}
			if err != nil {
				pc.close(fmt.Errorf("write error: %w", err))
				return nil, pc.mapRoundTripError(req, startBytesWritten, err)
			}
			//if d := pc.t.ResponseHeaderTimeout; d > 0 {
			//	if debugRoundTrip {
			//		//req.logf("starting timer for %v", d)
			//	}
			//	timer := time.NewTimer(d)
			//	defer timer.Stop() // prevent leaks
			//	respHeaderTimer = timer.C
			//}
		case <-pcClosed:
			pcClosed = nil
			if canceled || pc.t.replaceReqCanceler(req.cancelKey, nil) {
				if debugRoundTrip {
					//req.logf("closech recv: %T %#v", pc.closed, pc.closed)
				}
				return nil, pc.mapRoundTripError(req, startBytesWritten, pc.closed)
			}
		//case <-respHeaderTimer:
		case re := <-resc:
			if (re.res == nil) == (re.err == nil) {
				println(1)
				return nil, fmt.Errorf("internal error: exactly one of res or err should be set; nil=%v", re.res == nil)
			}
			if debugRoundTrip {
				println(2)
				//req.logf("resc recv: %p, %T/%#v", re.res, re.err, re.err)
			}
			if re.err != nil {
				println(3)
				return nil, pc.mapRoundTripError(req, startBytesWritten, re.err)
			}
			return re.res, nil
		// TODO(spongehah) cancel(pc.roundTrip)
		//case <-cancelChan:
		case <-req.Request.timeoutch:
			return nil, fmt.Errorf("request timeout\n")
		}
	}
}

// readWriteLoop handles the main I/O loop for a persistent connection.
// It processes incoming requests, sends them to the server, and handles responses.
func (pc *persistConn) readWriteLoop(loop *libuv.Loop) {
	defer close(pc.writeLoopDone)

	const debugReadWriteLoop = true // Debug switch provided for developers

	if debugReadWriteLoop {
		println("readWriteLoop start")
	}

	// The polling state machine!
	// Poll all ready tasks and act on them...
	alive := true
	var bodyWriter *io.PipeWriter
	var rw readWaiter
	for alive {
		select {
		case <-pc.closech:
			if debugReadWriteLoop {
				println("closech")
			}
			return
		default:
			task := pc.exec.Poll()
			if task == nil {
				loop.Run(libuv.RUN_ONCE)
				continue
			}
			taskId := (taskId)(uintptr(task.Userdata()))
			if debugReadWriteLoop {
				println(taskId)
			}
			switch taskId {
			case write:
				if debugReadWriteLoop {
					println("write")
				}
				wr := <-pc.writech // blocking

				startBytesWritten := pc.nwrite

				err := checkTaskType(task, write)
				if err != nil {
					wr.ch <- err
					// Free the resources
					//freeResources(task, respBody, bodyWriter, pc.exec, pc, rc)
					pc.close(err)
					return
				}

				client := (*hyper.ClientConn)(task.Value())
				task.Free()

				// Prepare the hyper.Request
				hyperReq, err := newHyperRequest(wr.req.Request)
				if err == nil {
					// Send it!
					sendTask := client.Send(hyperReq)
					setTaskId(sendTask, read)
					sendRes := pc.exec.Push(sendTask)
					if sendRes != hyper.OK {
						err = errors.New("failed to send the request")
					}
				}
				if bre, ok := err.(requestBodyReadError); ok {
					err = bre.error
					// Errors reading from the user's
					// Request.Body are high priority.
					// Set it here before sending on the
					// channels below or calling
					// pc.close() which tears down
					// connections and causes other
					// errors.
					wr.req.setError(err)
				}
				if err != nil {
					if pc.nwrite == startBytesWritten {
						err = nothingWrittenError{err}
					}
					//pc.writeErrCh <- err // to the body reader, which might recycle us
					wr.ch <- err // to the roundTrip function
					// Free the resources
					//freeResources(task, respBody, bodyWriter, pc.exec, pc, rc)
					pc.close(err)
					return
				}

				// For this example, no longer need the client
				client.Free()
				if debugReadWriteLoop {
					println("write end")
				}
			case read:
				if debugReadWriteLoop {
					println("read")
				}

				rc := <-pc.reqch // blocking

				closeErr := errReadLoopExiting // default value, if not changed below
				defer func() {
					pc.close(closeErr)
					// TODO(spongehah) ConnPool(readWriteLoop)
					//pc.t.removeIdleConn(pc)
				}()

				//tryPutIdleConn := func(trace *httptrace.ClientTrace) bool {
				//	if err := pc.t.tryPutIdleConn(pc); err != nil {
				//		closeErr = err
				//		if trace != nil && trace.PutIdleConn != nil && err != errKeepAlivesDisabled {
				//			trace.PutIdleConn(err)
				//		}
				//		return false
				//	}
				//	if trace != nil && trace.PutIdleConn != nil {
				//		trace.PutIdleConn(nil)
				//	}
				//	return true
				//}

				// eofc is used to block caller goroutines reading from Response.Body
				// at EOF until this goroutines has (potentially) added the connection
				// back to the idle pool.
				eofc := make(chan struct{}, 1)
				defer close(eofc) // unblock reader on errors

				// Read this once, before loop starts. (to avoid races in tests)
				testHookMu.Lock()
				testHookReadLoopBeforeNextRead := testHookReadLoopBeforeNextRead
				testHookMu.Unlock()

				pc.mu.Lock()
				if pc.numExpectedResponses == 0 {
					pc.closeLocked(errServerClosedIdle)
					pc.mu.Unlock()
					return
				}
				pc.mu.Unlock()

				err := checkTaskType(task, read)
				// Take the results
				hyperResp := (*hyper.Response)(task.Value())
				task.Free()

				var resp *Response
				var respBody *hyper.Body
				if err == nil {
					resp, err = ReadResponse(hyperResp, rc.req)
					respBody = hyperResp.Body()
					resp.Body, bodyWriter = io.Pipe()
				} else {
					err = transportReadFromServerError{err}
					closeErr = err
				}

				if err != nil {
					select {
					case rc.ch <- responseAndError{err: err}:
					case <-rc.callerGone:
						return
					}
					// Free the resources
					freeResources(task, respBody, bodyWriter, pc.exec, pc, rc)
					return
				}

				// Response has been returned, stop the timer
				if rc.req.timeout > 0 {
					pc.conn.TimeoutTimer.Stop()
					(*libuv.Handle)(c.Pointer(&pc.conn.TimeoutTimer)).Close(nil)
				}

				pc.mu.Lock()
				pc.numExpectedResponses--
				pc.mu.Unlock()

				bodyWritable := resp.bodyIsWritable()
				hasBody := rc.req.Method != "HEAD" && resp.ContentLength != 0

				if resp.Close || rc.req.Close || resp.StatusCode <= 199 || bodyWritable {
					// Don't do keep-alive on error if either party requested a close
					// or we get an unexpected informational (1xx) response.
					// StatusCode 100 is already handled above.
					alive = false
				}

				if !hasBody || bodyWritable {
					//replaced := pc.t.replaceReqCanceler(rc.cancelKey, nil)
					pc.t.replaceReqCanceler(rc.cancelKey, nil)

					// TODO(spongehah) ConnPool(readWriteLoop)
					//// Put the idle conn back into the pool before we send the response
					//// so if they process it quickly and make another request, they'll
					//// get this same conn. But we use the unbuffered channel 'rc'
					//// to guarantee that persistConn.roundTrip got out of its select
					//// potentially waiting for this persistConn to close.
					//alive = alive &&
					//	!pc.sawEOF &&
					//	pc.wroteRequest() &&
					//	replaced && tryPutIdleConn(trace)

					if bodyWritable {
						closeErr = errCallerOwnsConn
					}

					select {
					case rc.ch <- responseAndError{res: resp}:
					case <-rc.callerGone:
						return
					}

					// Now that they've read from the unbuffered channel, they're safely
					// out of the select that also waits on this goroutine to die, so
					// we're allowed to exit now if needed (if alive is false)
					testHookReadLoopBeforeNextRead()
					continue
				}

				waitForBodyRead := make(chan bool, 2)
				body := &bodyEOFSignal{
					body: resp.Body,
					earlyCloseFn: func() error {
						waitForBodyRead <- false
						<-eofc // will be closed by deferred call at the end of the function
						return nil
					},
					fn: func(err error) error {
						isEOF := err == io.EOF
						waitForBodyRead <- isEOF
						if isEOF {
							<-eofc // see comment above eofc declaration
						} else if err != nil {
							if cerr := pc.canceled(); cerr != nil {
								return cerr
							}
						}
						return err
					},
				}
				resp.Body = body

				// TODO(spongehah) gzip fail
				if rc.addedGzip && EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
					resp.Body = &gzipReader{body: body}
					resp.Header.Del("Content-Encoding")
					resp.Header.Del("Content-Length")
					resp.ContentLength = -1
					resp.Uncompressed = true
				}

				rw.waitForBodyRead = waitForBodyRead
				rw.rc = rc
				rw.eofc = eofc
				bodyForeachTask := respBody.Foreach(appendToResponseBody, c.Pointer(bodyWriter))
				setTaskId(bodyForeachTask, readDone)
				pc.exec.Push(bodyForeachTask)

				// TODO(spongehah) select blocking
				//select {
				//case rc.ch <- responseAndError{res: resp}:
				//case <-rc.callerGone:
				//	return
				//}
				rc.ch <- responseAndError{res: resp}

				// No longer need the response
				hyperResp.Free()
				if debugReadWriteLoop {
					println("read end")
				}

				//pc.t.replaceReqCanceler(rc.cancelKey, nil)
				//eofc <- struct{}{}
			case readDone:
				// A background task of reading the response body is completed
				if debugReadWriteLoop {
					println("readDone")
				}
				err := checkTaskType(task, readDone)
				if err != nil {
					fmt.Println(err)
					pc.close(err)
					alive = false
				}

				if task.Type() != hyper.TaskEmpty {
					err = errors.New("unexpected task type\n")
					fmt.Println(err)
					pc.close(err)
					return
				}

				// free the task
				task.Free()
				bodyWriter.Close()

				// Before looping back to the top of this function and peeking on
				// the bufio.Reader, wait for the caller goroutine to finish
				// reading the response body. (or for cancellation or death)
				rc := rw.rc
				select {
				//case bodyEOF := <-rw.waitForBodyRead:
				case <-rw.waitForBodyRead:
					//replaced := pc.t.replaceReqCanceler(rc.cancelKey, nil) // before pc might return to idle pool
					pc.t.replaceReqCanceler(rc.cancelKey, nil) // before pc might return to idle pool
					// TODO(spongehah) ConnPool(readWriteLoop)
					//alive = alive &&
					//	bodyEOF &&
					//	!pc.sawEOF &&
					//	pc.wroteRequest() &&
					//	replaced && tryPutIdleConn(trace)

					rw.eofc <- struct{}{}
				// TODO(spongehah) cancel(pc.readWriteLoop)
				//case <-rc.req.Cancel:
				//	alive = false
				//	pc.t.CancelRequest(rc.req)
				//case <-rc.req.Context().Done():
				//	alive = false
				//	pc.t.cancelRequest(rc.cancelKey, rc.req.Context().Err())
				case <-pc.closech:
					alive = false
				}

				testHookReadLoopBeforeNextRead()
				if debugReadWriteLoop {
					println("readDone end")
				}
			case notSet:
				// A background task for hyper_client completed...
				task.Free()
			}
		}
	}
}

// ----------------------------------------------------------

type connData struct {
	TcpHandle     libuv.Tcp
	ConnectReq    libuv.Connect
	ReadBuf       libuv.Buf
	TimeoutTimer  libuv.Timer
	ReadBufFilled uintptr
	ReadWaker     *hyper.Waker
	WriteWaker    *hyper.Waker
}

func (conn *connData) Close() error {
	freeConnData(conn)
	return nil
}

// onConnect is the libuv callback for a successful connection
func onConnect(req *libuv.Connect, status c.Int) {
	//conn := (*ConnData)(req.Data)
	//conn := (*struct{ data *ConnData })(c.Pointer(req)).data
	conn := (*connData)((*libuv.Req)(c.Pointer(req)).GetData())

	if status < 0 {
		c.Fprintf(c.Stderr, c.Str("connect error: %d\n"), libuv.Strerror(libuv.Errno(status)))
		return
	}
	(*libuv.Stream)(c.Pointer(&conn.TcpHandle)).StartRead(allocBuffer, onRead)
}

// allocBuffer allocates a buffer for reading from a socket
func allocBuffer(handle *libuv.Handle, suggestedSize uintptr, buf *libuv.Buf) {
	//conn := (*ConnData)(handle.Data)
	//conn := (*struct{ data *ConnData })(c.Pointer(handle)).data
	conn := (*connData)(handle.GetData())
	if conn.ReadBuf.Base == nil {
		conn.ReadBuf = libuv.InitBuf((*c.Char)(c.Malloc(suggestedSize)), c.Uint(suggestedSize))
		//base := make([]byte, suggestedSize)
		//conn.ReadBuf = libuv.InitBuf((*c.Char)(c.Pointer(&base[0])), c.Uint(suggestedSize))
		conn.ReadBufFilled = 0
	}
	*buf = libuv.InitBuf((*c.Char)(c.Pointer(uintptr(c.Pointer(conn.ReadBuf.Base))+conn.ReadBufFilled)), c.Uint(suggestedSize-conn.ReadBufFilled))
}

// onRead is the libuv callback for reading from a socket
// This callback function is called when data is available to be read
func onRead(stream *libuv.Stream, nread c.Long, buf *libuv.Buf) {
	// Get the connection data associated with the stream
	conn := (*connData)((*libuv.Handle)(c.Pointer(stream)).GetData())

	// If data was read (nread > 0)
	if nread > 0 {
		// Update the amount of filled buffer
		conn.ReadBufFilled += uintptr(nread)
	}
	// If there's a pending read waker
	if conn.ReadWaker != nil {
		// Wake up the pending read operation of Hyper
		conn.ReadWaker.Wake()
		// Clear the waker reference
		conn.ReadWaker = nil
	}
}

// readCallBack read callback function for Hyper library
func readCallBack(userdata c.Pointer, ctx *hyper.Context, buf *uint8, bufLen uintptr) uintptr {
	// Get the user data (connection data)
	conn := (*connData)(userdata)

	// If there's data in the buffer
	if conn.ReadBufFilled > 0 {
		// Calculate how much data to copy (minimum of filled amount and requested amount)
		var toCopy uintptr
		if bufLen < conn.ReadBufFilled {
			toCopy = bufLen
		} else {
			toCopy = conn.ReadBufFilled
		}
		// Copy data from read buffer to Hyper's buffer
		c.Memcpy(c.Pointer(buf), c.Pointer(conn.ReadBuf.Base), toCopy)
		// Move remaining data to the beginning of the buffer
		c.Memmove(c.Pointer(conn.ReadBuf.Base), c.Pointer(uintptr(c.Pointer(conn.ReadBuf.Base))+toCopy), conn.ReadBufFilled-toCopy)
		// Update the amount of filled buffer
		conn.ReadBufFilled -= toCopy
		// Return the number of bytes copied
		return toCopy
	}

	// If no data in buffer, set up a waker to wait for more data
	// Free the old waker if it exists
	if conn.ReadWaker != nil {
		conn.ReadWaker.Free()
	}
	// Create a new waker
	conn.ReadWaker = ctx.Waker()
	// Return HYPER_IO_PENDING to indicate operation is pending, waiting for more data
	return hyper.IoPending
}

// onWrite is the libuv callback for writing to a socket
// Callback function called after a write operation completes
func onWrite(req *libuv.Write, status c.Int) {
	// Get the connection data associated with the write request
	conn := (*connData)((*libuv.Req)(c.Pointer(req)).GetData())

	// If there's a pending write waker
	if conn.WriteWaker != nil {
		// Wake up the pending write operation
		conn.WriteWaker.Wake()
		// Clear the waker reference
		conn.WriteWaker = nil
	}
}

// writeCallBack write callback function for Hyper library
func writeCallBack(userdata c.Pointer, ctx *hyper.Context, buf *uint8, bufLen uintptr) uintptr {
	// Get the user data (connection data)
	conn := (*connData)(userdata)
	// Create a libuv buffer
	initBuf := libuv.InitBuf((*c.Char)(c.Pointer(buf)), c.Uint(bufLen))
	req := &libuv.Write{}
	// Associate the connection data with the write request
	(*libuv.Req)(c.Pointer(req)).SetData(c.Pointer(conn))

	// Perform the asynchronous write operation
	ret := req.Write((*libuv.Stream)(c.Pointer(&conn.TcpHandle)), &initBuf, 1, onWrite)
	// If the write operation was successfully initiated
	if ret >= 0 {
		// Return the number of bytes to be written
		return bufLen
	}

	// If the write operation can't complete immediately, set up a waker to wait for completion
	if conn.WriteWaker != nil {
		// Free the old waker if it exists
		conn.WriteWaker.Free()
	}
	// Create a new waker
	conn.WriteWaker = ctx.Waker()
	// Return HYPER_IO_PENDING to indicate operation is pending, waiting for write to complete
	return hyper.IoPending
}

// onTimeout is the libuv callback for a timeout
func onTimeout(handle *libuv.Timer) {
	ct := (*connAndTimeoutChan)((*libuv.Handle)(c.Pointer(handle)).GetData())
	close(ct.timeoutch)
	// Close the timer
	(*libuv.Handle)(c.Pointer(&ct.conn.TimeoutTimer)).Close(nil)
}

// newIoWithConnReadWrite creates a new IO with read and write callbacks
func newIoWithConnReadWrite(connData *connData) *hyper.Io {
	hyperIo := hyper.NewIo()
	hyperIo.SetUserdata(c.Pointer(connData))
	hyperIo.SetRead(readCallBack)
	hyperIo.SetWrite(writeCallBack)
	return hyperIo
}

// taskId The unique identifier of the next task polled from the executor
type taskId c.Int

const (
	notSet taskId = iota
	write
	read
	readDone
)

type readWaiter struct {
	rc              requestAndChan
	waitForBodyRead chan bool
	eofc            chan struct{}
}

// setTaskId Set taskId to the task's userdata as a unique identifier
func setTaskId(task *hyper.Task, userData taskId) {
	var data = userData
	task.SetUserdata(unsafe.Pointer(uintptr(data)))
}

// checkTaskType checks the task type
func checkTaskType(task *hyper.Task, curTaskId taskId) error {
	switch curTaskId {
	case write:
		if task.Type() == hyper.TaskError {
			c.Printf(c.Str("handshake task error!\n"))
			return fail((*hyper.Error)(task.Value()))
		}
		if task.Type() != hyper.TaskClientConn {
			return fmt.Errorf("unexpected task type\n")
		}
		return nil
	case read:
		if task.Type() == hyper.TaskError {
			c.Printf(c.Str("write task error!\n"))
			return fail((*hyper.Error)(task.Value()))
		}
		if task.Type() != hyper.TaskResponse {
			c.Printf(c.Str("unexpected task type\n"))
			return fmt.Errorf("unexpected task type\n")
		}
		return nil
	case readDone:
		if task.Type() == hyper.TaskError {
			c.Printf(c.Str("read error!\n"))
			return fail((*hyper.Error)(task.Value()))
		}
		return nil
	case notSet:
	}
	return fmt.Errorf("unexpected TaskId\n")
}

// fail prints the error details and panics
func fail(err *hyper.Error) error {
	if err != nil {
		c.Printf(c.Str("error code: %d\n"), err.Code())
		// grab the error details
		var errBuf [256]c.Char
		errLen := err.Print((*uint8)(c.Pointer(&errBuf[:][0])), uintptr(len(errBuf)))

		c.Printf(c.Str("details: %.*s\n"), c.Int(errLen), c.Pointer(&errBuf[:][0]))

		// clean up the error
		err.Free()
		return fmt.Errorf("hyper request error, error code: %d\n", int(err.Code()))
	}
	return nil
}

// freeResources frees the resources
func freeResources(task *hyper.Task, respBody *hyper.Body, bodyWriter *io.PipeWriter, exec *hyper.Executor, pc *persistConn, rc requestAndChan) {
	// Cleaning up before exiting
	if task != nil {
		task.Free()
	}
	if respBody != nil {
		respBody.Free()
	}
	if bodyWriter != nil {
		bodyWriter.Close()
	}
	if exec != nil {
		exec.Free()
	}
	(*libuv.Handle)(c.Pointer(&pc.conn.TcpHandle)).Close(nil)
	freeConnData(pc.conn)

	closeChannels(rc, pc)
}

// closeChannels closes the channels
func closeChannels(rc requestAndChan, pc *persistConn) {
	// Closing the channel
	close(rc.ch)
	close(pc.reqch)
}

// freeConnData frees the connection data
func freeConnData(conn *connData) {
	if conn.ReadWaker != nil {
		conn.ReadWaker.Free()
		conn.ReadWaker = nil
	}
	if conn.WriteWaker != nil {
		conn.WriteWaker.Free()
		conn.WriteWaker = nil
	}
	if conn.ReadBuf.Base != nil {
		c.Free(c.Pointer(conn.ReadBuf.Base))
		conn.ReadBuf.Base = nil
	}
}

// ----------------------------------------------------------

// error values for debugging and testing, not seen by users.
var (
	errKeepAlivesDisabled = errors.New("http: putIdleConn: keep alives disabled")
	errConnBroken         = errors.New("http: putIdleConn: connection is in bad state")
	errCloseIdle          = errors.New("http: putIdleConn: CloseIdleConnections was called")
	errTooManyIdle        = errors.New("http: putIdleConn: too many idle connections")
	errTooManyIdleHost    = errors.New("http: putIdleConn: too many idle connections for host")
	errCloseIdleConns     = errors.New("http: CloseIdleConnections called")
	errReadLoopExiting    = errors.New("http: persistConn.readLoop exiting")
	errIdleConnTimeout    = errors.New("http: idle connection timeout")

	// errServerClosedIdle is not seen by users for idempotent requests, but may be
	// seen by a user if the server shuts down an idle connection and sends its FIN
	// in flight with already-written POST body bytes from the client.
	// See https://github.com/golang/go/issues/19943#issuecomment-355607646
	errServerClosedIdle = errors.New("http: server closed idle connection")
)

// ErrSkipAltProtocol is a sentinel error value defined by Transport.RegisterProtocol.
var ErrSkipAltProtocol = errors.New("net/http: skip alternate protocol")
var errCannotRewind = errors.New("net/http: cannot rewind body after connection loss")

// errRequestCanceled is set to be identical to the one from h2 to facilitate
// testing.
var errRequestCanceled = http2errRequestCanceled

// errRequestCanceled is a copy of net/http's errRequestCanceled because it's not
// exported. At least they'll be DeepEqual for h1-vs-h2 comparisons tests.
var http2errRequestCanceled = errors.New("net/http: request canceled")
var errRequestCanceledConn = errors.New("net/http: request canceled while waiting for connection") // TODO: unify?
// errCallerOwnsConn is an internal sentinel error used when we hand
// off a writable response.Body to the caller. We use this to prevent
// closing a net.Conn that is now owned by the caller.
var errCallerOwnsConn = errors.New("read loop ending; caller owns writable underlying conn")

type httpError struct {
	err     string
	timeout bool
}

func (e *httpError) Error() string   { return e.err }
func (e *httpError) Timeout() bool   { return e.timeout }
func (e *httpError) Temporary() bool { return true }

// fakeLocker is a sync.Locker which does nothing. It's used to guard
// test-only fields when not under test, to avoid runtime atomic
// overhead.
type fakeLocker struct{}

func (fakeLocker) Lock()   {}
func (fakeLocker) Unlock() {}

// nothingWrittenError wraps a write errors which ended up writing zero bytes.
type nothingWrittenError struct {
	error
}

func (nwe nothingWrittenError) Unwrap() error {
	return nwe.error
}

// transportReadFromServerError is used by Transport.readLoop when the
// 1 byte peek read fails and we're actually anticipating a response.
// Usually this is just due to the inherent keep-alive shut down race,
// where the server closed the connection at the same time the client
// wrote. The underlying err field is usually io.EOF or some
// ECONNRESET sort of thing which varies by platform. But it might be
// the user's custom net.Conn.Read error too, so we carry it along for
// them to return from Transport.RoundTrip.
type transportReadFromServerError struct {
	err error
}

func (e transportReadFromServerError) Unwrap() error { return e.err }
func (e transportReadFromServerError) Error() string {
	return fmt.Sprintf("net/http: Transport failed to read from server: %v", e.err)
}

func nop() {}

// testHooks. Always non-nil.
var (
	testHookEnterRoundTrip   = nop
	testHookWaitResLoop      = nop
	testHookRoundTripRetried = nop
	testHookPrePendingDial   = nop
	testHookPostPendingDial  = nop

	testHookMu                     sync.Locker = fakeLocker{} // guards following
	testHookReadLoopBeforeNextRead             = nop
)

var portMap = map[string]string{
	"http":   "80",
	"https":  "443",
	"socks5": "1080",
}

func idnaASCIIFromURL(url *url.URL) string {
	addr := url.Hostname()
	if v, err := idnaASCII(addr); err == nil {
		addr = v
	}
	return addr
}

// canonicalAddr returns url.Host but always with a ":port" suffix.
func canonicalAddr(url *url.URL) string {
	port := url.Port()
	if port == "" {
		port = portMap[url.Scheme]
	}
	return xnet.JoinHostPort(idnaASCIIFromURL(url), port)
}

// persistConn wraps a connection, usually a persistent one
// (but may be used for non-keep-alive requests as well)
type persistConn struct {
	// alt optionally specifies the TLS NextProto RoundTripper.
	// This is used for HTTP/2 today and future protocols later.
	// If it's non-nil, the rest of the fields are unused.
	alt RoundTripper

	//br      *bufio.Reader       // from conn
	//bw      *bufio.Writer       // to conn
	//nwrite  int64               // bytes written
	//writech chan writeRequest   // written by roundTrip; read by writeLoop
	//closech chan struct{}       // closed when conn closed

	t             *Transport
	cacheKey      connectMethodKey
	conn          *connData
	nwrite        int64               // bytes written
	reqch         chan requestAndChan // written by roundTrip; read by readWriteLoop
	writech       chan writeRequest   // written by roundTrip; read by writeLoop(Already merged into reqch)
	closech       chan struct{}       // closed when conn closed
	writeLoopDone chan struct{}       // closed when write loop ends

	isProxy              bool
	mu                   sync.Mutex // guards following fields
	numExpectedResponses int
	closed               error // set non-nil when conn is closed, before closech is closed
	canceledErr          error // set non-nil if conn is canceled
	broken               bool  // an error has happened on this connection; marked broken so it's not reused.
	// mutateHeaderFunc is an optional func to modify extra
	// headers on each outbound request before it's written. (the
	// original Request given to RoundTrip is not modified)
	mutateHeaderFunc func(Header)

	// hyper specific
	exec *hyper.Executor
	opts *hyper.ClientConnOptions
	io   *hyper.Io
}

func (pc *persistConn) cancelRequest(err error) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.canceledErr = err
	pc.closeLocked(errRequestCanceled)
}

// close closes the underlying TCP connection and closes
// the pc.closech channel.
//
// The provided err is only for testing and debugging; in normal
// circumstances it should never be seen by users.
func (pc *persistConn) close(err error) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.closeLocked(err)
}

func (pc *persistConn) closeLocked(err error) {
	if err == nil {
		panic("nil error")
	}
	pc.broken = true
	if pc.closed == nil {
		pc.closed = err
		pc.t.decConnsPerHost(pc.cacheKey)
		// Close HTTP/1 (pc.alt == nil) connection.
		// HTTP/2 closes its connection itself.
		if pc.alt == nil {
			if err != errCallerOwnsConn {
				pc.conn.Close()
			}
			close(pc.closech)
		}
	}
	pc.mutateHeaderFunc = nil
}

// mapRoundTripError returns the appropriate error value for
// persistConn.roundTrip.
//
// The provided err is the first error that (*persistConn).roundTrip
// happened to receive from its select statement.
//
// The startBytesWritten value should be the value of pc.nwrite before the roundTrip
// started writing the request.
func (pc *persistConn) mapRoundTripError(req *transportRequest, startBytesWritten int64, err error) error {
	if err == nil {
		return nil
	}

	// Wait for the writeLoop goroutine to terminate to avoid data
	// races on callers who mutate the request on failure.
	//
	// When resc in pc.roundTrip and hence rc.ch receives a responseAndError
	// with a non-nil error it implies that the persistConn is either closed
	// or closing. Waiting on pc.writeLoopDone is hence safe as all callers
	// close closech which in turn ensures writeLoop returns.
	<-pc.writeLoopDone

	// If the request was canceled, that's better than network
	// failures that were likely the result of tearing down the
	// connection.
	if cerr := pc.canceled(); cerr != nil {
		return cerr
	}

	// See if an error was set explicitly.
	req.mu.Lock()
	reqErr := req.err
	req.mu.Unlock()
	if reqErr != nil {
		return reqErr
	}

	if err == errServerClosedIdle {
		// Don't decorate
		return err
	}

	if _, ok := err.(transportReadFromServerError); ok {
		if pc.nwrite == startBytesWritten {
			return nothingWrittenError{err}
		}
		// Don't decorate
		return err
	}
	if pc.isBroken() {
		if pc.nwrite == startBytesWritten {
			return nothingWrittenError{err}
		}
		return fmt.Errorf("net/http: HTTP/1.x transport connection broken: %w", err)
	}
	return err
}

// canceled returns non-nil if the connection was closed due to
// CancelRequest or due to context cancellation.
func (pc *persistConn) canceled() error {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	return pc.canceledErr
}

// isBroken reports whether this connection is in a known broken state.
func (pc *persistConn) isBroken() bool {
	pc.mu.Lock()
	b := pc.closed != nil
	pc.mu.Unlock()
	return b
}

// connectMethod is the map key (in its String form) for keeping persistent
// TCP connections alive for subsequent HTTP requests.
//
// A connect method may be of the following types:
//
//	connectMethod.key().String()      Description
//	------------------------------    -------------------------
//	|http|foo.com                     http directly to server, no proxy
//	|https|foo.com                    https directly to server, no proxy
//	|https,h1|foo.com                 https directly to server w/o HTTP/2, no proxy
//	http://proxy.com|https|foo.com    http to proxy, then CONNECT to foo.com
//	http://proxy.com|http             http to proxy, http to anywhere after that
//	socks5://proxy.com|http|foo.com   socks5 to proxy, then http to foo.com
//	socks5://proxy.com|https|foo.com  socks5 to proxy, then https to foo.com
//	https://proxy.com|https|foo.com   https to proxy, then CONNECT to foo.com
//	https://proxy.com|http            https to proxy, http to anywhere after that
type connectMethod struct {
	_            incomparable
	proxyURL     *url.URL // nil for no proxy, else full proxy URL
	targetScheme string   // "http" or "https"
	// If proxyURL specifies an http or https proxy, and targetScheme is http (not https),
	// then targetAddr is not included in the connect method key, because the socket can
	// be reused for different targetAddr values.
	targetAddr string
	treq       *transportRequest // optional
	onlyH1     bool              // whether to disable HTTP/2 and force HTTP/1
}

// connectMethodKey is the map key version of connectMethod, with a
// stringified proxy URL (or the empty string) instead of a pointer to
// a URL.
type connectMethodKey struct {
	proxy, scheme, addr string
	onlyH1              bool
}

func (cm *connectMethod) key() connectMethodKey {
	proxyStr := ""
	targetAddr := cm.targetAddr
	if cm.proxyURL != nil {
		proxyStr = cm.proxyURL.String()
		if (cm.proxyURL.Scheme == "http" || cm.proxyURL.Scheme == "https") && cm.targetScheme == "http" {
			targetAddr = ""
		}
	}
	return connectMethodKey{
		proxy:  proxyStr,
		scheme: cm.targetScheme,
		addr:   targetAddr,
		onlyH1: cm.onlyH1,
	}
}

// scheme returns the first hop scheme: http, https, or socks5
func (cm *connectMethod) scheme() string {
	if cm.proxyURL != nil {
		return cm.proxyURL.Scheme
	}
	return cm.targetScheme
}

// addr returns the first hop "host:port" to which we need to TCP connect.
func (cm *connectMethod) addr() string {
	if cm.proxyURL != nil {
		return canonicalAddr(cm.proxyURL)
	}
	return cm.targetAddr
}

// proxyAuth returns the Proxy-Authorization header to set
// on requests, if applicable.
func (cm *connectMethod) proxyAuth() string {
	if cm.proxyURL == nil {
		return ""
	}
	if u := cm.proxyURL.User; u != nil {
		username := u.Username()
		password, _ := u.Password()
		return "Basic " + basicAuth(username, password)
	}
	return ""
}

// A wantConn records state about a wanted connection
// (that is, an active call to getConn).
// The conn may be gotten by dialing or by finding an idle connection,
// or a cancellation may make the conn no longer wanted.
// These three options are racing against each other and use
// wantConn to coordinate and agree about the winning outcome.
type wantConn struct {
	cm    connectMethod
	key   connectMethodKey // cm.key()
	ctx   context.Context  // context for dial
	ready chan struct{}    // closed when pc, err pair is delivered

	// hooks for testing to know when dials are done
	// beforeDial is called in the getConn goroutine when the dial is queued.
	// afterDial is called when the dial is completed or canceled.
	beforeDial func()
	afterDial  func()

	mu  sync.Mutex // protects pc, err, close(ready)
	pc  *persistConn
	err error
}

// cancel marks w as no longer wanting a result (for example, due to cancellation).
// If a connection has been delivered already, cancel returns it with t.putOrCloseIdleConn.
func (w *wantConn) cancel(t *Transport, err error) {
	w.mu.Lock()
	if w.pc == nil && w.err == nil {
		close(w.ready) // catch misbehavior in future delivery
	}
	//pc := w.pc
	w.pc = nil
	w.err = err
	w.mu.Unlock()

	// TODO(spongehah) ConnPool
	//if pc != nil {
	//	t.putOrCloseIdleConn(pc)
	//}
}

// waiting reports whether w is still waiting for an answer (connection or error).
func (w *wantConn) waiting() bool {
	select {
	case <-w.ready:
		return false
	default:
		return true
	}
}

// tryDeliver attempts to deliver pc, err to w and reports whether it succeeded.
func (w *wantConn) tryDeliver(pc *persistConn, err error) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.pc != nil || w.err != nil {
		return false
	}

	w.pc = pc
	w.err = err
	if w.pc == nil && w.err == nil {
		panic("net/http: internal error: misuse of tryDeliver")
	}
	close(w.ready)
	return true
}

// A wantConnQueue is a queue of wantConns.
type wantConnQueue struct {
	// This is a queue, not a deque.
	// It is split into two stages - head[headPos:] and tail.
	// popFront is trivial (headPos++) on the first stage, and
	// pushBack is trivial (append) on the second stage.
	// If the first stage is empty, popFront can swap the
	// first and second stages to remedy the situation.
	//
	// This two-stage split is analogous to the use of two lists
	// in Okasaki's purely functional queue but without the
	// overhead of reversing the list when swapping stages.
	head    []*wantConn
	headPos int
	tail    []*wantConn
}

// len returns the number of items in the queue.
func (q *wantConnQueue) len() int {
	return len(q.head) - q.headPos + len(q.tail)
}

// pushBack adds w to the back of the queue.
func (q *wantConnQueue) pushBack(w *wantConn) {
	q.tail = append(q.tail, w)
}

// popFront removes and returns the wantConn at the front of the queue.
func (q *wantConnQueue) popFront() *wantConn {
	if q.headPos >= len(q.head) {
		if len(q.tail) == 0 {
			return nil
		}
		// Pick up tail as new head, clear tail.
		q.head, q.headPos, q.tail = q.tail, 0, q.head[:0]
	}
	w := q.head[q.headPos]
	q.head[q.headPos] = nil
	q.headPos++
	return w
}

// peekFront returns the wantConn at the front of the queue without removing it.
func (q *wantConnQueue) peekFront() *wantConn {
	if q.headPos < len(q.head) {
		return q.head[q.headPos]
	}
	if len(q.tail) > 0 {
		return q.tail[0]
	}
	return nil
}

// cleanFront pops any wantConns that are no longer waiting from the head of the
// queue, reporting whether any were popped.
func (q *wantConnQueue) cleanFront() (cleaned bool) {
	for {
		w := q.peekFront()
		if w == nil || w.waiting() {
			return cleaned
		}
		q.popFront()
		cleaned = true
	}
}

// bodyEOFSignal is used by the HTTP/1 transport when reading response
// bodies to make sure we see the end of a response body before
// proceeding and reading on the connection again.
//
// It wraps a ReadCloser but runs fn (if non-nil) at most
// once, right before its final (error-producing) Read or Close call
// returns. fn should return the new error to return from Read or Close.
//
// If earlyCloseFn is non-nil and Close is called before io.EOF is
// seen, earlyCloseFn is called instead of fn, and its return value is
// the return value from Close.
type bodyEOFSignal struct {
	body         io.ReadCloser
	mu           sync.Mutex        // guards following 4 fields
	closed       bool              // whether Close has been called
	rerr         error             // sticky Read error
	fn           func(error) error // err will be nil on Read io.EOF
	earlyCloseFn func() error      // optional alt Close func used if io.EOF not seen
}

var errReadOnClosedResBody = errors.New("http: read on closed response body")

func (es *bodyEOFSignal) Read(p []byte) (n int, err error) {
	es.mu.Lock()
	closed, rerr := es.closed, es.rerr
	es.mu.Unlock()
	if closed {
		return 0, errReadOnClosedResBody
	}
	if rerr != nil {
		return 0, rerr
	}

	n, err = es.body.Read(p)
	if err != nil {
		es.mu.Lock()
		defer es.mu.Unlock()
		if es.rerr == nil {
			es.rerr = err
		}
		err = es.condfn(err)
	}
	return
}

func (es *bodyEOFSignal) Close() error {
	es.mu.Lock()
	defer es.mu.Unlock()
	if es.closed {
		return nil
	}
	es.closed = true
	if es.earlyCloseFn != nil && es.rerr != io.EOF {
		return es.earlyCloseFn()
	}
	err := es.body.Close()
	return es.condfn(err)
}

// caller must hold es.mu.
func (es *bodyEOFSignal) condfn(err error) error {
	if es.fn == nil {
		return err
	}
	err = es.fn(err)
	es.fn = nil
	return err
}

// gzipReader wraps a response body so it can lazily
// call gzip.NewReader on the first call to Read
type gzipReader struct {
	_    incomparable
	body *bodyEOFSignal // underlying HTTP/1 response body framing
	zr   *gzip.Reader   // lazily-initialized gzip reader
	zerr error          // any error from gzip.NewReader; sticky
}

func (gz *gzipReader) Read(p []byte) (n int, err error) {
	if gz.zr == nil {
		if gz.zerr == nil {
			gz.zr, gz.zerr = gzip.NewReader(gz.body)
		}
		if gz.zerr != nil {
			return 0, gz.zerr
		}
	}

	gz.body.mu.Lock()
	if gz.body.closed {
		err = errReadOnClosedResBody
	}
	gz.body.mu.Unlock()

	if err != nil {
		return 0, err
	}
	return gz.zr.Read(p)
}

func (gz *gzipReader) Close() error {
	return gz.body.Close()
}
