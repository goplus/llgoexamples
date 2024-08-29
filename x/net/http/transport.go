package http

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/libuv"
	cnet "github.com/goplus/llgo/c/net"
	"github.com/goplus/llgo/c/syscall"
	"github.com/goplus/llgo/x/net"
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
const debugSwitch = true

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

	// libuv and hyper related
	loopInitOnce sync.Once
	loop         *libuv.Loop
	exec         *hyper.Executor
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

// responseAndError is how the goroutine reading from an HTTP/1 server
// communicates with the goroutine doing the RoundTrip.
type responseAndError struct {
	_   incomparable
	res *Response // else use this response (see res method)
	err error
}

type timeoutData struct {
	timeoutch chan struct{}
	taskData  *taskData
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

func (t *Transport) close(err error) {
	t.reqMu.Lock()
	defer t.reqMu.Unlock()
	t.closeLocked(err)
}

func (t *Transport) closeLocked(err error) {
	if err != nil {
		fmt.Println(err)
	}
	if t.loop != nil {
		t.loop.Close()
	}
	if t.exec != nil {
		t.exec.Free()
	}
}

func getMilliseconds(deadline time.Time) uint64 {
	microseconds := deadline.Sub(time.Now()).Microseconds()
	milliseconds := microseconds / 1e3
	if microseconds%1e3 != 0 {
		milliseconds += 1
	}
	return uint64(milliseconds)
}

// ----------------------------------------------------------

func (t *Transport) RoundTrip(req *Request) (*Response, error) {
	if debugSwitch {
		println("RoundTrip start")
		defer println("RoundTrip end")
	}
	t.loopInitOnce.Do(func() {
		t.loop = libuv.LoopNew()
		t.exec = hyper.NewExecutor()

		//idle := &libuv.Idle{}
		//libuv.InitIdle(t.loop, idle)
		//(*libuv.Handle)(c.Pointer(idle)).SetData(c.Pointer(t))
		//idle.Start(readWriteLoop)

		checker := &libuv.Check{}
		libuv.InitCheck(t.loop, checker)
		(*libuv.Handle)(c.Pointer(checker)).SetData(c.Pointer(t))
		checker.Start(readWriteLoop)

		go t.loop.Run(libuv.RUN_DEFAULT)
	})

	// If timeout is set, start the timer
	var didTimeout func() bool
	var stopTimer func()
	// Only the first request will initialize the timer
	if req.timer == nil && !req.deadline.IsZero() {
		req.timer = &libuv.Timer{}
		req.timeoutch = make(chan struct{}, 1)
		libuv.InitTimer(t.loop, req.timer)
		ch := &timeoutData{
			timeoutch: req.timeoutch,
			taskData:  nil,
		}
		(*libuv.Handle)(c.Pointer(req.timer)).SetData(c.Pointer(ch))

		req.timer.Start(onTimeout, getMilliseconds(req.deadline), 0)
		if debugSwitch {
			println("timer start")
		}
		didTimeout = func() bool { return req.timer.GetDueIn() == 0 }
		stopTimer = func() {
			close(req.timeoutch)
			req.timer.Stop()
			(*libuv.Handle)(c.Pointer(req.timer)).Close(nil)
			if debugSwitch {
				println("timer close")
			}
		}
	} else {
		didTimeout = alwaysFalse
		stopTimer = nop
	}

	resp, err := t.doRoundTrip(req)
	if err != nil {
		stopTimer()
		return nil, err
	}

	if !req.deadline.IsZero() {
		resp.Body = &cancelTimerBody{
			stop:          stopTimer,
			rc:            resp.Body,
			reqDidTimeout: didTimeout,
		}
	}
	return resp, nil
}

func (t *Transport) doRoundTrip(req *Request) (*Response, error) {
	if debugSwitch {
		println("doRoundTrip start")
		defer println("doRoundTrip end")
	}
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
		// TODO(spongehah) timeout(t.doRoundTrip)
		//select {
		//case <-ctx.Done():
		//	req.closeBody()
		//	return nil, ctx.Err()
		//default:
		//}
		select {
		case <-req.timeoutch:
			req.closeBody()
			return nil, errors.New("request timeout!")
		default:
		}

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
		// TODO(spongehah) Retry & ConnPool(t.doRoundTrip)
		return nil, err
	}
}

func (t *Transport) getConn(treq *transportRequest, cm connectMethod) (pc *persistConn, err error) {
	if debugSwitch {
		println("getConn start")
		defer println("getConn end")
	}
	req := treq.Request
	//trace := treq.trace
	//ctx := req.Context()
	//if trace != nil && trace.GetConn != nil {
	//	trace.GetConn(cm.addr())
	//}

	w := &wantConn{
		cm:  cm,
		key: cm.key(),
		//ctx:        ctx,
		timeoutch:  treq.timeoutch,
		ready:      make(chan struct{}, 1),
		beforeDial: testHookPrePendingDial,
		afterDial:  testHookPostPendingDial,
	}
	defer func() {
		if err != nil {
			w.cancel(t, err)
		}
	}()

	// TODO(spongehah) ConnPool(t.getConn)
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
			// TODO(spongehah) cancel(t.getConn)
			//case <-req.Cancel:
			//	return nil, errRequestCanceledConn
			//case <-req.Context().Done():
			//	return nil, req.Context().Err()
			case <-req.timeoutch:
				return nil, errors.New("timeout: req.Context().Err()")
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
	//case <-req.Context().Done():
	//	return nil,
	case <-req.timeoutch:
		if debugSwitch {
			println("getConn: timeoutch")
		}
		return nil, errors.New("timeout: req.Context().Err()\n")
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
	if debugSwitch {
		println("queueForDial start")
		defer println("queueForDial end")
	}
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
	if debugSwitch {
		println("dialConnFor start")
		defer println("dialConnFor end")
	}
	defer w.afterDial()

	pc, err := t.dialConn(w.timeoutch, w.cm)
	w.tryDeliver(pc, err)
	// TODO(spongehah) ConnPool(t.dialConnFor)
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

func (t *Transport) dialConn(timeoutch chan struct{}, cm connectMethod) (pconn *persistConn, err error) {
	if debugSwitch {
		println("dialConn start")
		defer println("dialConn end")
	}
	select {
	case <-timeoutch:
		return
	default:
	}
	pconn = &persistConn{
		t:             t,
		cacheKey:      cm.key(),
		closech:       make(chan struct{}, 1),
		writeLoopDone: make(chan struct{}, 1),
	}

	//trace := httptrace.ContextClientTrace(ctx)
	//wrapErr := func(err error) error {
	//	if cm.proxyURL != nil {
	//		// Return a typed error, per Issue 16997
	//		return &net.OpError{Op: "proxyconnect", Net: "tcp", Err: err}
	//	}
	//	return err
	//}
	//
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
	conn, err := t.dial(timeoutch, cm.addr())
	if err != nil {
		return nil, err
	}
	pconn.conn = conn

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
	select {
	case <-timeoutch:
		conn.Close()
		return
	default:
	}
	// TODO(spongehah) Proxy(https/sock5)(t.dialConn)
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

	select {
	case <-timeoutch:
		conn.Close()
		return
	default:
	}
	return pconn, nil
}

func (t *Transport) dial(timeoutch chan struct{}, addr string) (*connData, error) {
	if debugSwitch {
		println("dial start")
		defer println("dial end")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	conn := new(connData)

	libuv.InitTcp(t.loop, &conn.TcpHandle)
	(*libuv.Handle)(c.Pointer(&conn.TcpHandle)).SetData(c.Pointer(conn))

	var hints cnet.AddrInfo
	c.Memset(c.Pointer(&hints), 0, unsafe.Sizeof(hints))
	hints.Family = syscall.AF_UNSPEC
	hints.SockType = syscall.SOCK_STREAM

	var res *cnet.AddrInfo
	status := cnet.Getaddrinfo(c.AllocaCStr(host), c.AllocaCStr(port), &hints, &res)
	if status != 0 {
		return nil, fmt.Errorf("getaddrinfo error\n")
	}

	(*libuv.Req)(c.Pointer(&conn.ConnectReq)).SetData(c.Pointer(conn))
	status = libuv.TcpConnect(&conn.ConnectReq, &conn.TcpHandle, res.Addr, onConnect)
	if status != 0 {
		return nil, fmt.Errorf("connect error: %s\n", c.GoString(libuv.Strerror(libuv.Errno(status))))
	}

	cnet.Freeaddrinfo(res)
	return conn, nil
}

func (pc *persistConn) roundTrip(req *transportRequest) (resp *Response, err error) {
	if debugSwitch {
		println("roundTrip start")
		defer println("roundTrip end")
	}
	testHookEnterRoundTrip()
	if !pc.t.replaceReqCanceler(req.cancelKey, pc.cancelRequest) {
		// TODO(spongehah) ConnPool(pc.roundTrip)
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
	// TODO(spongehah) gzip(pc.roundTrip)
	//if !pc.t.DisableCompression &&
	//	req.Header.Get("Accept-Encoding") == "" &&
	//	req.Header.Get("Range") == "" &&
	//	req.Method != "HEAD" {
	//	// Request gzip only, not deflate. Deflate is ambiguous and
	//	// not as universally supported anyway.
	//	// See: https://zlib.net/zlib_faq.html#faq39
	//	//
	//	// Note that we don't request this for HEAD requests,
	//	// due to a bug in nginx:
	//	//   https://trac.nginx.org/nginx/ticket/358
	//	//   https://golang.org/issue/5522
	//	//
	//	// We don't request gzip if the request is for a range, since
	//	// auto-decoding a portion of a gzipped document will just fail
	//	// anyway. See https://golang.org/issue/8923
	//	requestedGzip = true
	//	req.extraHeaders().Set("Accept-Encoding", "gzip")
	//}

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

	// Write the request concurrently with waiting for a response,
	// in case the server decides to reply before reading our full
	// request body.
	startBytesWritten := pc.conn.nwrite
	writeErrCh := make(chan error, 1)
	resc := make(chan responseAndError, 1)

	// Hookup the IO
	hyperIo := newIoWithConnReadWrite(pc.conn)
	// We need an executor generally to poll futures
	// Prepare client options
	opts := hyper.NewClientConnOptions()
	opts.Exec(pc.t.exec)
	// send the handshake
	handshakeTask := hyper.Handshake(hyperIo, opts)
	taskData := &taskData{
		taskId:     write,
		req:        req,
		pc:         pc,
		addedGzip:  requestedGzip,
		writeErrCh: writeErrCh,
		callerGone: gone,
		resc:       resc,
	}
	handshakeTask.SetUserdata(c.Pointer(taskData))
	// Send the request to readWriteLoop().
	// Let's wait for the handshake to finish...

	pc.t.exec.Push(handshakeTask)
	async := &libuv.Async{}
	pc.t.loop.Async(async, asyncCb)
	async.Send()

	//var respHeaderTimer <-chan time.Time
	//cancelChan := req.Request.Cancel
	//ctxDoneChan := req.Context().Done()
	timeoutch := req.timeoutch
	pcClosed := pc.closech
	canceled := false

	for {
		testHookWaitResLoop()
		if debugSwitch {
			println("roundTrip for")
		}
		select {
		case err := <-writeErrCh:
			if debugSwitch {
				println("roundTrip: writeErrch")
			}
			if err != nil {
				pc.close(fmt.Errorf("write error: %w", err))
				if pc.conn.nwrite == startBytesWritten {
					err = nothingWrittenError{err}
				}
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
			if debugSwitch {
				println("roundTrip: pcClosed")
			}
			pcClosed = nil
			if canceled || pc.t.replaceReqCanceler(req.cancelKey, nil) {
				return nil, pc.mapRoundTripError(req, startBytesWritten, pc.closed)
			}
		//case <-respHeaderTimer:
		case re := <-resc:
			if debugSwitch {
				println("roundTrip: resc")
			}
			if (re.res == nil) == (re.err == nil) {
				return nil, fmt.Errorf("internal error: exactly one of res or err should be set; nil=%v", re.res == nil)
			}
			if re.err != nil {
				return nil, pc.mapRoundTripError(req, startBytesWritten, re.err)
			}
			return re.res, nil
		// TODO(spongehah) cancel(pc.roundTrip)
		//case <-cancelChan:
		//	canceled = pc.t.cancelRequest(req.cancelKey, errRequestCanceled)
		//	cancelChan = nil
		//case <-ctxDoneChan:
		//	canceled = pc.t.cancelRequest(req.cancelKey, req.Context().Err())
		//	cancelChan = nil
		//	ctxDoneChan = nil
		case <-timeoutch:
			if debugSwitch {
				println("roundTrip: timeoutch")
			}
			canceled = pc.t.cancelRequest(req.cancelKey, errors.New("timeout: req.Context().Err()"))
			timeoutch = nil
			return nil, errors.New("request timeout")
		}
	}
}

func asyncCb(async *libuv.Async) {
	println("async called")
}

// readWriteLoop handles the main I/O loop for a persistent connection.
// It processes incoming requests, sends them to the server, and handles responses.
func readWriteLoop(idle *libuv.Check) {
	println("polling")
	t := (*Transport)((*libuv.Handle)(c.Pointer(idle)).GetData())

	// Read this once, before loop starts. (to avoid races in tests)
	testHookMu.Lock()
	testHookReadLoopBeforeNextRead := testHookReadLoopBeforeNextRead
	testHookMu.Unlock()

	const debugReadWriteLoop = true // Debug switch provided for developers

	// The polling state machine!
	// Poll all ready tasks and act on them...
	for {
		task := t.exec.Poll()
		if task == nil {
			return
		}
		taskData := (*taskData)(task.Userdata())
		var taskId taskId
		if taskData != nil {
			taskId = taskData.taskId
		} else {
			taskId = notSet
		}
		if debugReadWriteLoop {
			println("taskId: ", taskId)
		}
		switch taskId {
		case write:
			if debugReadWriteLoop {
				println("write")
			}

			select {
			case <-taskData.pc.closech:
				task.Free()
				continue
			default:
			}

			err := checkTaskType(task, write)
			client := (*hyper.ClientConn)(task.Value())
			task.Free()

			if err == nil {
				// TODO(spongehah) Proxy(writeLoop)
				err = taskData.req.Request.write(client, taskData, t.exec)
			}
			// For this request, no longer need the client
			client.Free()
			if bre, ok := err.(requestBodyReadError); ok {
				err = bre.error
				// Errors reading from the user's
				// Request.Body are high priority.
				// Set it here before sending on the
				// channels below or calling
				// pc.close() which tears down
				// connections and causes other
				// errors.
				taskData.req.setError(err)
			}
			if err != nil {
				//pc.writeErrCh <- err // to the body reader, which might recycle us
				taskData.writeErrCh <- err // to the roundTrip function
				taskData.pc.close(err)
				continue
			}

			if debugReadWriteLoop {
				println("write end")
			}
		case read:
			if debugReadWriteLoop {
				println("read")
			}

			if taskData.pc.closeErr == nil {
				taskData.pc.closeErr = errReadLoopExiting
			}
			// TODO(spongehah) ConnPool(readWriteLoop)
			//if taskData.pc.tryPutIdleConn == nil {
			//	//taskData.pc.tryPutIdleConn := func(trace *httptrace.ClientTrace) bool {
			//	//	if err := pc.t.tryPutIdleConn(pc); err != nil {
			//	//		closeErr = err
			//	//		if trace != nil && trace.PutIdleConn != nil && err != errKeepAlivesDisabled {
			//	//			trace.PutIdleConn(err)
			//	//		}
			//	//		return false
			//	//	}
			//	//	if trace != nil && trace.PutIdleConn != nil {
			//	//		trace.PutIdleConn(nil)
			//	//	}
			//	//	return true
			//	//}
			//}

			err := checkTaskType(task, read)

			taskData.pc.mu.Lock()
			if taskData.pc.numExpectedResponses == 0 {
				taskData.pc.closeLocked(errServerClosedIdle)
				taskData.pc.mu.Unlock()

				// defer
				taskData.pc.close(taskData.pc.closeErr)
				// TODO(spongehah) ConnPool(readWriteLoop)
				//t.removeIdleConn(pc)
				continue
			}
			taskData.pc.mu.Unlock()

			//trace := httptrace.ContextClientTrace(rc.req.Context())

			// Take the results
			hyperResp := (*hyper.Response)(task.Value())
			task.Free()

			var resp *Response
			var respBody *hyper.Body
			if err == nil {
				var pr *io.PipeReader
				pr, taskData.bodyWriter = io.Pipe()
				resp, err = ReadResponse(pr, taskData.req.Request, hyperResp)
				respBody = hyperResp.Body()
			} else {
				err = transportReadFromServerError{err}
				taskData.pc.closeErr = err
			}

			// No longer need the response
			hyperResp.Free()

			if err != nil {
				select {
				case taskData.resc <- responseAndError{err: err}:
				case <-taskData.callerGone:
					// defer
					taskData.pc.close(taskData.pc.closeErr)
					// TODO(spongehah) ConnPool(readWriteLoop)
					//t.removeIdleConn(pc)
					continue
				}
				// defer
				taskData.pc.close(taskData.pc.closeErr)
				// TODO(spongehah) ConnPool(readWriteLoop)
				//t.removeIdleConn(pc)
				continue
			}

			taskData.pc.mu.Lock()
			taskData.pc.numExpectedResponses--
			taskData.pc.mu.Unlock()

			bodyWritable := resp.bodyIsWritable()
			hasBody := taskData.req.Method != "HEAD" && resp.ContentLength != 0

			if resp.Close || taskData.req.Close || resp.StatusCode <= 199 || bodyWritable {
				// Don't do keep-alive on error if either party requested a close
				// or we get an unexpected informational (1xx) response.
				// StatusCode 100 is already handled above.
				taskData.pc.alive = false
			}

			if !hasBody || bodyWritable {
				//replaced := pc.t.replaceReqCanceler(rc.cancelKey, nil)
				t.replaceReqCanceler(taskData.req.cancelKey, nil)

				// TODO(spongehah) ConnPool(readWriteLoop)
				//// Put the idle conn back into the pool before we send the response
				//// so if they process it quickly and make another request, they'll
				//// get this same conn. But we use the unbuffered channel 'rc'
				//// to guarantee that persistConn.roundTrip got out of its select
				//// potentially waiting for this persistConn to close.
				//taskData.pc.alive = taskData.pc.alive &&
				//	!pc.sawEOF &&
				//	pc.wroteRequest() &&
				//	replaced && tryPutIdleConn(trace)

				if bodyWritable {
					taskData.pc.closeErr = errCallerOwnsConn
				}

				select {
				case taskData.resc <- responseAndError{res: resp}:
				case <-taskData.callerGone:
					// defer
					taskData.pc.close(taskData.pc.closeErr)
					// TODO(spongehah) ConnPool(readWriteLoop)
					//t.removeIdleConn(pc)
					continue
				}
				// Now that they've read from the unbuffered channel, they're safely
				// out of the select that also waits on this goroutine to die, so
				// we're allowed to exit now if needed (if alive is false)
				testHookReadLoopBeforeNextRead()
				if taskData.pc.alive == false {
					// defer
					taskData.pc.close(taskData.pc.closeErr)
					// TODO(spongehah) ConnPool(readWriteLoop)
					//t.removeIdleConn(pc)
				}
				continue
			}

			body := &bodyEOFSignal{
				body: resp.Body,
				earlyCloseFn: func() error {
					taskData.bodyWriter.Close()
					return nil
				},
				fn: func(err error) error {
					isEOF := err == io.EOF
					if !isEOF {
						if cerr := taskData.pc.canceled(); cerr != nil {
							return cerr
						}
					}
					return err
				},
			}
			resp.Body = body

			// TODO(spongehah) gzip(pc.readWriteLoop)
			//if taskData.addedGzip && EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
			//	println("gzip reader")
			//	resp.Body = &gzipReader{body: body}
			//	resp.Header.Del("Content-Encoding")
			//	resp.Header.Del("Content-Length")
			//	resp.ContentLength = -1
			//	resp.Uncompressed = true
			//}

			bodyForeachTask := respBody.Foreach(appendToResponseBody, c.Pointer(taskData.bodyWriter))
			taskData.taskId = readDone
			bodyForeachTask.SetUserdata(c.Pointer(taskData))
			t.exec.Push(bodyForeachTask)
			(*timeoutData)((*libuv.Handle)(c.Pointer(taskData.req.timer)).GetData()).taskData = taskData

			// TODO(spongehah) select blocking(readWriteLoop)
			//select {
			//case taskData.resc <- responseAndError{res: resp}:
			//case <-taskData.callerGone:
			//	// defer
			//	taskData.pc.close(taskData.pc.closeErr)
			//	// TODO(spongehah) ConnPool(readWriteLoop)
			//	//t.removeIdleConn(pc)
			//	continue
			//}
			select {
			case <-taskData.callerGone:
				// defer
				taskData.pc.close(taskData.pc.closeErr)
				// TODO(spongehah) ConnPool(readWriteLoop)
				//t.removeIdleConn(pc)
				continue
			default:
			}
			taskData.resc <- responseAndError{res: resp}

			if debugReadWriteLoop {
				println("read end")
			}
		case readDone:
			// A background task of reading the response body is completed
			if debugReadWriteLoop {
				println("readDone")
			}
			if taskData.bodyWriter != nil {
				taskData.bodyWriter.Close()
			}
			checkTaskType(task, readDone)

			//bodyEOF := task.Type() == hyper.TaskEmpty
			// free the task
			task.Free()

			t.replaceReqCanceler(taskData.req.cancelKey, nil) // before pc might return to idle pool
			// TODO(spongehah) ConnPool(readWriteLoop)
			//taskData.pc.alive = taskData.pc.alive &&
			//	bodyEOF &&
			//	!pc.sawEOF &&
			//	pc.wroteRequest() &&
			//	replaced && tryPutIdleConn(trace)

			// TODO(spongehah) cancel(pc.readWriteLoop)
			//case <-rw.rc.req.Cancel:
			//	taskData.pc.alive = false
			//	pc.t.CancelRequest(rw.rc.req)
			//case <-rw.rc.req.Context().Done():
			//	taskData.pc.alive = false
			//	pc.t.cancelRequest(rw.rc.cancelKey, rw.rc.req.Context().Err())
			//case <-taskData.pc.closech:
			//	taskData.pc.alive = false
			//}

			select {
			case <-taskData.req.timeoutch:
				continue
			case <-taskData.pc.closech:
				taskData.pc.alive = false
			default:
			}

			if taskData.pc.alive == false {
				// defer
				taskData.pc.close(taskData.pc.closeErr)
				// TODO(spongehah) ConnPool(readWriteLoop)
				//t.removeIdleConn(pc)
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

// ----------------------------------------------------------

type taskData struct {
	taskId     taskId
	bodyWriter *io.PipeWriter
	req        *transportRequest
	pc         *persistConn
	addedGzip  bool
	writeErrCh chan error
	callerGone chan struct{}
	resc       chan responseAndError
}

type connData struct {
	TcpHandle     libuv.Tcp
	ConnectReq    libuv.Connect
	ReadBuf       libuv.Buf
	ReadBufFilled uintptr
	nwrite        int64 // bytes written(Replaced from persistConn's nwrite)
	ReadWaker     *hyper.Waker
	WriteWaker    *hyper.Waker
}

func (conn *connData) Close() error {
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
	(*libuv.Handle)(c.Pointer(&conn.TcpHandle)).Close(nil)
	return nil
}

// onConnect is the libuv callback for a successful connection
func onConnect(req *libuv.Connect, status c.Int) {
	if debugSwitch {
		println("connect start")
		defer println("connect end")
	}
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
		conn.nwrite += int64(bufLen)
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
func onTimeout(timer *libuv.Timer) {
	if debugSwitch {
		println("onTimeout start")
		defer println("onTimeout end")
	}
	data := (*timeoutData)((*libuv.Handle)(c.Pointer(timer)).GetData())
	close(data.timeoutch)
	timer.Stop()

	taskData := data.taskData
	if taskData != nil {
		pc := taskData.pc
		pc.alive = false
		pc.t.cancelRequest(taskData.req.cancelKey, errors.New("timeout: req.Context().Err()"))
		// defer
		pc.close(pc.closeErr)
		// TODO(spongehah) ConnPool(onTimeout)
		//t.removeIdleConn(pc)
	}
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

// checkTaskType checks the task type
func checkTaskType(task *hyper.Task, curTaskId taskId) error {
	switch curTaskId {
	case write:
		if task.Type() == hyper.TaskError {
			log.Printf("[readWriteLoop::write]handshake task error!\n")
			return fail((*hyper.Error)(task.Value()))
		}
		if task.Type() != hyper.TaskClientConn {
			return fmt.Errorf("[readWriteLoop::write]unexpected task type\n")
		}
		return nil
	case read:
		if task.Type() == hyper.TaskError {
			log.Printf("[readWriteLoop::read]write task error!\n")
			return fail((*hyper.Error)(task.Value()))
		}
		if task.Type() != hyper.TaskResponse {
			c.Printf(c.Str("[readWriteLoop::read]unexpected task type\n"))
			return errors.New("[readWriteLoop::read]unexpected task type\n")
		}
		return nil
	case readDone:
		if task.Type() == hyper.TaskError {
			log.Printf("[readWriteLoop::readDone]read response body error!\n")
			return fail((*hyper.Error)(task.Value()))
		}
		return nil
	case notSet:
	}
	return errors.New("[readWriteLoop]unexpected task type\n")
}

// fail prints the error details and panics
func fail(err *hyper.Error) error {
	if err != nil {
		c.Printf(c.Str("[readWriteLoop]error code: %d\n"), err.Code())
		// grab the error details
		var errBuf [256]c.Char
		errLen := err.Print((*uint8)(c.Pointer(&errBuf[:][0])), uintptr(len(errBuf)))

		c.Printf(c.Str("[readWriteLoop]details: %.*s\n"), c.Int(errLen), c.Pointer(&errBuf[:][0]))

		// clean up the error
		err.Free()
		return fmt.Errorf("[readWriteLoop]hyper request error, error code: %d\n", int(err.Code()))
	}
	return nil
}

// ----------------------------------------------------------

// error values for debugging and testing, not seen by users.
var (
	errKeepAlivesDisabled   = errors.New("http: putIdleConn: keep alives disabled")
	errConnBroken           = errors.New("http: putIdleConn: connection is in bad state")
	errCloseIdle            = errors.New("http: putIdleConn: CloseIdleConnections was called")
	errTooManyIdle          = errors.New("http: putIdleConn: too many idle connections")
	errTooManyIdleHost      = errors.New("http: putIdleConn: too many idle connections for host")
	errCloseIdleConns       = errors.New("http: CloseIdleConnections called")
	errReadLoopExiting      = errors.New("http: Transport.readWriteLoop.read exiting")
	errReadWriteLoopExiting = errors.New("http: Transport.readWriteLoop exiting")
	errIdleConnTimeout      = errors.New("http: idle connection timeout")

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
	return net.JoinHostPort(idnaASCIIFromURL(url), port)
}

// persistConn wraps a connection, usually a persistent one
// (but may be used for non-keep-alive requests as well)
type persistConn struct {
	// alt optionally specifies the TLS NextProto RoundTripper.
	// This is used for HTTP/2 today and future protocols later.
	// If it's non-nil, the rest of the fields are unused.
	alt RoundTripper

	t        *Transport
	cacheKey connectMethodKey
	conn     *connData
	//tlsState *tls.ConnectionState
	//nwrite        int64       // bytes written(Replaced by connData.nwrite)
	closech chan struct{} // closed when conn closed
	isProxy bool

	writeLoopDone chan struct{} // closed when readWriteLoop ends

	mu                   sync.Mutex // guards following fields
	numExpectedResponses int
	closed               error // set non-nil when conn is closed, before closech is closed
	canceledErr          error // set non-nil if conn is canceled
	broken               bool  // an error has happened on this connection; marked broken so it's not reused.
	// mutateHeaderFunc is an optional func to modify extra
	// headers on each outbound request before it's written. (the
	// original Request given to RoundTrip is not modified)
	mutateHeaderFunc func(Header)

	// other
	alive    bool  // Replace the alive in readLoop
	closeErr error // Replace the closeErr in readLoop
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
			close(pc.writeLoopDone)
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
		if pc.conn.nwrite == startBytesWritten {
			return nothingWrittenError{err}
		}
		// Don't decorate
		return err
	}
	if pc.isBroken() {
		if pc.conn.nwrite == startBytesWritten {
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
	onlyH1     bool // whether to disable HTTP/2 and force HTTP/1
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
	cm        connectMethod
	key       connectMethodKey // cm.key()
	ctx       context.Context  // context for dial
	timeoutch chan struct{}    // tmp timeout to replace ctx
	ready     chan struct{}    // closed when pc, err pair is delivered

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

	// TODO(spongehah) ConnPool(w.cancel)
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
	select {
	case <-w.timeoutch:
		pc.close(errors.New("request timeout: dialConn timeout"))
	default:
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
