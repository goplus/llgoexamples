package http

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
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
	"github.com/goplus/llgoexamples/rust/hyper"
	"github.com/goplus/llgoexamples/x/net"
)

// DefaultTransport is the default implementation of Transport and is
// used by DefaultClient. It establishes network connections as needed
// and caches them for reuse by subsequent calls. It uses HTTP proxies
// as directed by the environment variables HTTP_PROXY, HTTPS_PROXY
// and NO_PROXY (or the lowercase versions thereof).
var DefaultTransport RoundTripper = &Transport{
	Proxy:           nil,
	MaxIdleConns:    100,
	IdleConnTimeout: 90 * time.Second,
}

// DefaultMaxIdleConnsPerHost is the default value of Transport's
// MaxIdleConnsPerHost.
const DefaultMaxIdleConnsPerHost = 2

// Debug switch provided for developers
const (
	debugSwitch        = true
	debugReadWriteLoop = true
)

type Transport struct {
	idleMu       sync.Mutex
	closeIdle    bool                                // user has requested to close all idle conns
	idleConn     map[connectMethodKey][]*persistConn // most recently used at end
	idleConnWait map[connectMethodKey]wantConnQueue  // waiting getConns
	idleLRU      connLRU

	reqMu       sync.Mutex
	reqCanceler map[cancelKey]func(error)

	altProto atomic.Value // of nil or map[string]RoundTripper, key is URI scheme

	connsPerHostMu   sync.Mutex
	connsPerHost     map[connectMethodKey]int
	connsPerHostWait map[connectMethodKey]wantConnQueue // waiting getConns

	Proxy func(*Request) (*url.URL, error)

	DisableKeepAlives  bool
	DisableCompression bool

	MaxIdleConns        int
	MaxIdleConnsPerHost int
	MaxConnsPerHost     int
	IdleConnTimeout     time.Duration

	loopsMu   sync.Mutex
	loops     []*clientEventLoop
	isClosing atomic.Bool
	//curLoop   atomic.Uint32
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
	clientTaskData  *clientTaskData
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

func (t *Transport) putOrCloseIdleConn(pconn *persistConn) {
	if err := t.tryPutIdleConn(pconn); err != nil {
		if debugSwitch {
			println("############### putOrCloseIdleConn: close")
		}
		pconn.close(err)
	}
}

func (t *Transport) maxIdleConnsPerHost() int {
	if v := t.MaxIdleConnsPerHost; v != 0 {
		return v
	}
	return DefaultMaxIdleConnsPerHost
}

// tryPutIdleConn adds pconn to the list of idle persistent connections awaiting
// a new request.
// If pconn is no longer needed or not in a good state, tryPutIdleConn returns
// an error explaining why it wasn't registered.
// tryPutIdleConn does not close pconn. Use putOrCloseIdleConn instead for that.
func (t *Transport) tryPutIdleConn(pconn *persistConn) error {
	if t.DisableKeepAlives || t.MaxIdleConnsPerHost < 0 {
		return errKeepAlivesDisabled
	}
	if pconn.isBroken() {
		return errConnBroken
	}
	pconn.markReused()

	t.idleMu.Lock()
	defer t.idleMu.Unlock()

	// HTTP/2 (pconn.alt != nil) connections do not come out of the idle list,
	// because multiple goroutines can use them simultaneously.
	// If this is an HTTP/2 connection being “returned,” we're done.
	if pconn.alt != nil && t.idleLRU.m[pconn] != nil {
		return nil
	}

	// Deliver pconn to goroutine waiting for idle connection, if any.
	// (They may be actively dialing, but this conn is ready first.
	// Chrome calls this socket late binding.
	// See https://www.chromium.org/developers/design-documents/network-stack#TOC-Connection-Management.)
	key := pconn.cacheKey
	if q, ok := t.idleConnWait[key]; ok {
		done := false
		if pconn.alt == nil {
			// HTTP/1.
			// Loop over the waiting list until we find a w that isn't done already, and hand it pconn.
			for q.len() > 0 {
				w := q.popFront()
				if w.tryDeliver(pconn, nil) {
					done = true
					break
				}
			}
		} else {
			// HTTP/2.
			// Can hand the same pconn to everyone in the waiting list,
			// and we still won't be done: we want to put it in the idle
			// list unconditionally, for any future clients too.
			for q.len() > 0 {
				w := q.popFront()
				w.tryDeliver(pconn, nil)
			}
		}
		if q.len() == 0 {
			delete(t.idleConnWait, key)
		} else {
			t.idleConnWait[key] = q
		}
		if done {
			return nil
		}
	}

	if t.closeIdle {
		return errCloseIdle
	}
	if t.idleConn == nil {
		t.idleConn = make(map[connectMethodKey][]*persistConn)
	}
	idles := t.idleConn[key]
	if len(idles) >= t.maxIdleConnsPerHost() {
		return errTooManyIdleHost
	}
	for _, exist := range idles {
		if exist == pconn {
			log.Fatalf("dup idle pconn %p in freelist", pconn)
		}
	}
	t.idleConn[key] = append(idles, pconn)
	t.idleLRU.add(pconn)
	if t.MaxIdleConns != 0 && t.idleLRU.len() > t.MaxIdleConns {
		oldest := t.idleLRU.removeOldest()
		if debugSwitch {
			println("############### tryPutIdleConn: removeOldest")
		}
		oldest.close(errTooManyIdle)
		t.removeIdleConnLocked(oldest)
	}

	// Set idle timer, but only for HTTP/1 (pconn.alt == nil).
	// The HTTP/2 implementation manages the idle timer itself
	// (see idleConnTimeout in h2_bundle.go).
	idleConnTimeout := uint64(t.IdleConnTimeout.Milliseconds())
	if t.IdleConnTimeout > 0 && pconn.alt == nil {
		if pconn.idleTimer != nil {
			pconn.idleTimer.Start(onIdleConnTimeout, idleConnTimeout, 0)
		} else {
			pconn.idleTimer = &libuv.Timer{}
			libuv.InitTimer(pconn.eventLoop.loop, pconn.idleTimer)
			(*libuv.Handle)(c.Pointer(pconn.idleTimer)).SetData(c.Pointer(pconn))
			pconn.idleTimer.Start(onIdleConnTimeout, idleConnTimeout, 0)
		}
	}
	pconn.idleAt = time.Now()
	return nil
}

func onIdleConnTimeout(timer *libuv.Timer) {
	pconn := (*persistConn)((*libuv.Handle)(c.Pointer(timer)).GetData())
	isClose := pconn.closeConnIfStillIdle()
	if isClose {
		timer.Stop()
	} else {
		timer.Start(onIdleConnTimeout, 0, 0)
	}
}

// queueForIdleConn queues w to receive the next idle connection for w.cm.
// As an optimization hint to the caller, queueForIdleConn reports whether
// it successfully delivered an already-idle connection.
func (t *Transport) queueForIdleConn(w *wantConn) (delivered bool) {
	if t.DisableKeepAlives {
		return false
	}

	t.idleMu.Lock()
	defer t.idleMu.Unlock()

	// Stop closing connections that become idle - we might want one.
	// (That is, undo the effect of t.CloseIdleConnections.)
	t.closeIdle = false

	if w == nil {
		// Happens in test hook.
		return false
	}

	// If IdleConnTimeout is set, calculate the oldest
	// persistConn.idleAt time we're willing to use a cached idle
	// conn.
	var oldTime time.Time
	if t.IdleConnTimeout > 0 {
		oldTime = time.Now().Add(-t.IdleConnTimeout)
	}
	// Look for most recently-used idle connection.
	if list, ok := t.idleConn[w.key]; ok {
		stop := false
		delivered := false
		for len(list) > 0 && !stop {
			pconn := list[len(list)-1]

			// See whether this connection has been idle too long, considering
			// only the wall time (the Round(0)), in case this is a laptop or VM
			// coming out of suspend with previously cached idle connections.
			// FIXME: Round() is not supported in llgo
			//tooOld := !oldTime.IsZero() && pconn.idleAt.Round(0).Before(oldTime)
			tooOld := !oldTime.IsZero() && pconn.idleAt.Before(oldTime)
			if tooOld {
				// Async cleanup. Launch in its own goroutine (as if a
				// time.AfterFunc called it); it acquires idleMu, which we're
				// holding, and does a synchronous net.Conn.Close.
				pconn.closeConnIfStillIdleLocked()
			}
			if pconn.isBroken() || tooOld {
				// If either persistConn.readLoop has marked the connection
				// broken, but Transport.removeIdleConn has not yet removed it
				// from the idle list, or if this persistConn is too old (it was
				// idle too long), then ignore it and look for another. In both
				// cases it's already in the process of being closed.
				list = list[:len(list)-1]
				continue
			}
			delivered = w.tryDeliver(pconn, nil)
			if delivered {
				if pconn.alt != nil {
					// HTTP/2: multiple clients can share pconn.
					// Leave it in the list.
				} else {
					// HTTP/1: only one client can use pconn.
					// Remove it from the list.
					t.idleLRU.remove(pconn)
					list = list[:len(list)-1]
				}
			}
			stop = true
		}
		if len(list) > 0 {
			t.idleConn[w.key] = list
		} else {
			delete(t.idleConn, w.key)
		}
		if stop {
			return delivered
		}
	}

	// Register to receive next connection that becomes idle.
	if t.idleConnWait == nil {
		t.idleConnWait = make(map[connectMethodKey]wantConnQueue)
	}
	q := t.idleConnWait[w.key]
	q.cleanFront()
	q.pushBack(w)
	t.idleConnWait[w.key] = q
	return false
}

// removeIdleConn marks pconn as dead.
func (t *Transport) removeIdleConn(pconn *persistConn) bool {
	t.idleMu.Lock()
	defer t.idleMu.Unlock()
	return t.removeIdleConnLocked(pconn)
}

// t.idleMu must be held.
func (t *Transport) removeIdleConnLocked(pconn *persistConn) bool {
	if pconn.idleTimer != nil && (*libuv.Handle)(c.Pointer(pconn.idleTimer)).IsClosing() == 0 {
		pconn.idleTimer.Stop()
		(*libuv.Handle)(c.Pointer(pconn.idleTimer)).Close(nil)
		pconn.idleTimer = nil
	}
	t.idleLRU.remove(pconn)
	key := pconn.cacheKey
	pconns := t.idleConn[key]
	var removed bool
	switch len(pconns) {
	case 0:
		// Nothing
	case 1:
		if pconns[0] == pconn {
			delete(t.idleConn, key)
			removed = true
		}
	default:
		for i, v := range pconns {
			if v != pconn {
				continue
			}
			// Slide down, keeping most recently-used
			// conns at the end.
			copy(pconns[i:], pconns[i+1:])
			t.idleConn[key] = pconns[:len(pconns)-1]
			removed = true
			break
		}
	}
	return removed
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

func (t *Transport) connectMethodForRequest(treq *transportRequest, loop *clientEventLoop) (cm connectMethod, err error) {
	cm.targetScheme = treq.URL.Scheme
	cm.targetAddr = canonicalAddr(treq.URL)
	if t.Proxy != nil {
		cm.proxyURL, err = t.Proxy(treq.Request)
	}
	cm.onlyH1 = treq.requiresHTTP1()
	cm.eventLoop = loop
	return cm, err
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
	// If this request requires HTTP/1, don't use the
	// "https" alternate protocol, which is used by the
	// HTTP/2 code to take over requests if there's an
	// existing cached HTTP/2 connection.
	return !(req.URL.Scheme == "https" && req.requiresHTTP1())
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

func (t *Transport) Close() {
	if t != nil && !t.isClosing.Swap(true) {
		t.CloseIdleConnections()
		for _, el := range t.loops {
			el.Close()
		}
	}
}

type clientEventLoop struct {
	// libuv and hyper related
	loop      *libuv.Loop
	async     *libuv.Async
	exec      *hyper.Executor
	isRunning atomic.Bool
	isClosing atomic.Bool
}

func (el *clientEventLoop) Close() {
	if el != nil && !el.isClosing.Swap(true) {
		if el.loop != nil && (*libuv.Handle)(c.Pointer(el.loop)).IsClosing() == 0 {
			el.loop.Close()
			el.loop = nil
		}
		if el.async != nil && (*libuv.Handle)(c.Pointer(el.async)).IsClosing() == 0 {
			el.async.Close(nil)
			el.async = nil
		}
		if el.exec != nil {
			el.exec.Free()
			el.exec = nil
		}
	}
}

func (el *clientEventLoop) run() {
	if el.isRunning.Load() {
		return
	}

	el.loop.Async(el.async, nil)

	checker := &libuv.Idle{}
	libuv.InitIdle(el.loop, checker)
	(*libuv.Handle)(c.Pointer(checker)).SetData(c.Pointer(el))
	checker.Start(readWriteLoop)

	go el.loop.Run(libuv.RUN_DEFAULT)

	el.isRunning.Store(true)
}

// ----------------------------------------------------------

func getMilliseconds(deadline time.Time) uint64 {
	microseconds := deadline.Sub(time.Now()).Microseconds()
	milliseconds := microseconds / 1e3
	if microseconds%1e3 != 0 {
		milliseconds += 1
	}
	return uint64(milliseconds)
}

func init() {
	cpuCount = int(c.Sysconf(_SC_NPROCESSORS_ONLN))
	if cpuCount <= 0 {
		cpuCount = 4
	}
}

func (t *Transport) getOrInitClientEventLoop(i uint32) *clientEventLoop {
	if el := t.loops[i]; el != nil {
		return el
	}

	eventLoop := &clientEventLoop{
		loop:  libuv.LoopNew(),
		async: &libuv.Async{},
		exec:  hyper.NewExecutor(),
	}

	eventLoop.run()

	t.loops[i] = eventLoop
	return eventLoop
}

func (t *Transport) getClientEventLoop(req *Request) *clientEventLoop {
	t.loopsMu.Lock()
	defer t.loopsMu.Unlock()
	if t.loops == nil {
		t.loops = make([]*clientEventLoop, cpuCount)
	}

	key := t.getLoopKey(req)
	h := fnv.New32a()
	h.Write([]byte(key))
	hashcode := h.Sum32()

	return t.getOrInitClientEventLoop(hashcode % uint32(cpuCount))
	//i := (t.curLoop.Add(1) - 1) % uint32(cpuCount)
	//return t.getOrInitClientEventLoop(i)
}

func (t *Transport) getLoopKey(req *Request) string {
	proxyStr := ""
	if t.Proxy != nil {
		proxyURL, _ := t.Proxy(req)
		proxyStr = proxyURL.String()
	}
	return req.URL.String() + proxyStr
}

func (t *Transport) RoundTrip(req *Request) (*Response, error) {
	if debugSwitch {
		println("############### RoundTrip start")
		defer println("############### RoundTrip end")
	}

	eventLoop := t.getClientEventLoop(req)

	// If timeout is set, start the timer
	var didTimeout func() bool
	var stopTimer func()
	// Only the first request will initialize the timer
	if req.timer == nil && !req.deadline.IsZero() {
		req.timer = &libuv.Timer{}
		libuv.InitTimer(eventLoop.loop, req.timer)
		ch := &timeoutData{
			timeoutch: req.timeoutch,
			clientTaskData:  nil,
		}
		(*libuv.Handle)(c.Pointer(req.timer)).SetData(c.Pointer(ch))

		req.timer.Start(onTimeout, getMilliseconds(req.deadline), 0)
		if debugSwitch {
			println("############### timer start")
		}
		didTimeout = func() bool { return req.timer.GetDueIn() == 0 }
		stopTimer = func() {
			close(req.timeoutch)
			req.timer.Stop()
			if (*libuv.Handle)(c.Pointer(req.timer)).IsClosing() == 0 {
				(*libuv.Handle)(c.Pointer(req.timer)).Close(nil)
			}
			if debugSwitch {
				println("############### timer close")
			}
		}
	} else {
		didTimeout = alwaysFalse
		stopTimer = nop
	}

	resp, err := t.doRoundTrip(req, eventLoop)
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

func (t *Transport) doRoundTrip(req *Request, loop *clientEventLoop) (*Response, error) {
	if debugSwitch {
		println("############### doRoundTrip start")
		defer println("############### doRoundTrip end")
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
		select {
		case <-req.timeoutch:
			req.closeBody()
			return nil, errors.New("request timeout!")
		default:
		}

		// treq gets modified by roundTrip, so we need to recreate for each retry.
		//treq := &transportRequest{Request: req, trace: trace, cancelKey: cancelKey}
		treq := &transportRequest{Request: req, cancelKey: cancelKey}
		cm, err := t.connectMethodForRequest(treq, loop)
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
			println("################# getConn err != nil")
			t.setReqCanceler(cancelKey, nil)
			req.closeBody()
			return nil, err
		}

		var resp *Response
		if pconn.alt != nil {
			// HTTP/2 path.
			t.setReqCanceler(cancelKey, nil) // HTTP/2 not cancelable with CancelRequest
			resp, err = pconn.alt.RoundTrip(req)
		} else {
			// HTTP/1.X path.
			resp, err = pconn.roundTrip(treq)
		}

		if err == nil {
			resp.Request = origReq
			return resp, nil
		}

		// Failed. Clean up and determine whether to retry.
		if http2isNoCachedConnError(err) {
			if t.removeIdleConn(pconn) {
				t.decConnsPerHost(pconn.cacheKey)
			}
		} else if !pconn.shouldRetryRequest(req, err) {
			// Issue 16465: return underlying net.Conn.Read error from peek,
			// as we've historically done.
			if e, ok := err.(nothingWrittenError); ok {
				err = e.error
			}
			if e, ok := err.(transportReadFromServerError); ok {
				err = e.err
			}
			if b, ok := req.Body.(*readTrackingBody); ok && !b.didClose {
				// Issue 49621: Close the request body if pconn.roundTrip
				// didn't do so already. This can happen if the pconn
				// write loop exits without reading the write request.
				req.closeBody()
			}
			return nil, err
		}
		testHookRoundTripRetried()

		// Rewind the body if we're able to.
		req, err = rewindBody(req)
		if err != nil {
			return nil, err
		}
	}
}

func (t *Transport) getConn(treq *transportRequest, cm connectMethod) (pc *persistConn, err error) {
	if debugSwitch {
		println("############### getConn start")
		defer println("############### getConn end")
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
		beforeDial: testHookPrePendingDial,
		afterDial:  testHookPostPendingDial,
	}
	defer func() {
		if err != nil {
			w.cancel(t, err)
		}
	}()

	// Queue for idle connection.
	if delivered := t.queueForIdleConn(w); delivered {
		pc := w.pc
		// Trace only for HTTP/1.
		// HTTP/2 calls trace.GotConn itself.
		//if pc.alt == nil && trace != nil && trace.GotConn != nil {
		//	trace.GotConn(pc.gotIdleConnTrace(pc.idleAt))
		//}
		// set request canceler to some non-nil function so we
		// can detect whether it was cleared between now and when
		// we enter roundTrip
		t.setReqCanceler(treq.cancelKey, func(error) {})
		return pc, nil
	}

	cancelc := make(chan error, 1)
	t.setReqCanceler(treq.cancelKey, func(err error) { cancelc <- err })

	// Queue for permission to dial.
	t.queueForDial(w)

	// Trace success but only for HTTP/1.
	// HTTP/2 calls trace.GotConn itself.
	//if w.pc != nil && w.pc.alt == nil && trace != nil && trace.GotConn != nil {
	//	trace.GotConn(httptrace.GotConnInfo{Conn: w.pc.conn, Reused: w.pc.isReused()})
	//}
	if w.err != nil {
		return nil, w.err
	}
	// If the request has been canceled, that's probably
	// what caused w.err; if so, prefer to return the
	// cancellation error (see golang.org/issue/16049).
	select {
	case <-req.timeoutch:
		if debugSwitch {
			println("############### getConn: timeoutch")
		}
		return nil, errors.New("timeout: req.Context().Err()")
	case err := <-cancelc:
		if err == errRequestCanceled {
			err = errRequestCanceledConn
		}
		return nil, err
	default:
		// return below
	}
	return w.pc, w.err
}

// queueForDial queues w to wait for permission to begin dialing.
// Once w receives permission to dial, it will do so in a separate goroutine.
func (t *Transport) queueForDial(w *wantConn) {
	if debugSwitch {
		println("############### queueForDial start")
		defer println("############### queueForDial end")
	}
	w.beforeDial()

	if t.MaxConnsPerHost <= 0 {
		t.dialConnFor(w)
		return
	}

	t.connsPerHostMu.Lock()
	defer t.connsPerHostMu.Unlock()

	if n := t.connsPerHost[w.key]; n < t.MaxConnsPerHost {
		if t.connsPerHost == nil {
			t.connsPerHost = make(map[connectMethodKey]int)
		}
		t.connsPerHost[w.key] = n + 1
		t.dialConnFor(w)
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
		println("############### dialConnFor start")
		defer println("############### dialConnFor end")
	}
	defer w.afterDial()

	pc, err := t.dialConn(w.timeoutch, w.cm)
	delivered := w.tryDeliver(pc, err)
	// If the connection was successfully established but was not passed to w,
	// or is a shareable HTTP/2 connection
	if err == nil && (!delivered || pc.alt != nil) {
		// pconn was not passed to w,
		// or it is HTTP/2 and can be shared.
		// Add to the idle connection pool.
		t.putOrCloseIdleConn(pc)
	}
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
				t.dialConnFor(w)
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
		println("############### dialConn start")
		defer println("############### dialConn end")
	}
	select {
	case <-timeoutch:
		err = errors.New("[t.dialConn] request timeout")
		return
	default:
	}
	pconn = &persistConn{
		t:             t,
		cacheKey:      cm.key(),
		closech:       make(chan struct{}, 1),
		writeLoopDone: make(chan struct{}, 1),
		alive:         true,
		chunkAsync:    &libuv.Async{},
		eventLoop:     cm.eventLoop,
	}
	cm.eventLoop.loop.Async(pconn.chunkAsync, readyToRead)

	conn, err := t.dial(cm)
	if err != nil {
		return nil, err
	}
	pconn.conn = conn

	select {
	case <-timeoutch:
		conn.Close()
		return
	default:
	}
	// TODO(hah) Proxy(https/sock5)(t.dialConn)
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

	pconn.closeErr = errReadLoopExiting

	select {
	case <-timeoutch:
		err = errors.New("[t.dialConn] request timeout")
		if debugSwitch {
			println("############### dialConn: timeoutch")
		}
		pconn.close(err)
		return nil, err
	default:
	}
	return pconn, nil
}

func (t *Transport) dial(cm connectMethod) (*connData, error) {
	if debugSwitch {
		println("############### dial start")
		defer println("############### dial end")
	}
	addr := cm.addr()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	conn := new(connData)

	libuv.InitTcp(cm.eventLoop.loop, &conn.tcpHandle)
	(*libuv.Handle)(c.Pointer(&conn.tcpHandle)).SetData(c.Pointer(conn))

	var hints cnet.AddrInfo
	c.Memset(c.Pointer(&hints), 0, unsafe.Sizeof(hints))
	hints.Family = syscall.AF_UNSPEC
	hints.SockType = syscall.SOCK_STREAM

	var res *cnet.AddrInfo
	status := cnet.Getaddrinfo(c.AllocaCStr(host), c.AllocaCStr(port), &hints, &res)
	if status != 0 {
		return nil, fmt.Errorf("getaddrinfo error\n")
	}

	(*libuv.Req)(c.Pointer(&conn.connectReq)).SetData(c.Pointer(conn))
	status = libuv.TcpConnect(&conn.connectReq, &conn.tcpHandle, res.Addr, onConnect)
	if status != 0 {
		return nil, fmt.Errorf("connect error: %s\n", c.GoString(libuv.Strerror(libuv.Errno(status))))
	}

	cnet.Freeaddrinfo(res)
	return conn, nil
}

func (pc *persistConn) roundTrip(req *transportRequest) (resp *Response, err error) {
	if debugSwitch {
		println("############### roundTrip start")
		defer println("############### roundTrip end")
	}
	testHookEnterRoundTrip()
	if !pc.t.replaceReqCanceler(req.cancelKey, pc.cancelRequest) {
		pc.t.putOrCloseIdleConn(pc)
		return nil, errRequestCanceled
	}
	pc.mu.Lock()
	pc.numExpectedResponses++
	headerFn := pc.mutateHeaderFunc
	pc.mu.Unlock()

	if headerFn != nil {
		headerFn(req.extraHeaders())
	}

	// Set extra headers, such as Accept-Encoding, Connection(Keep-Alive).
	requestedGzip := pc.setExtraHeaders(req)

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

	clientTaskData := &clientTaskData{
		req:        req,
		pc:         pc,
		addedGzip:  requestedGzip,
		writeErrCh: writeErrCh,
		callerGone: gone,
		resc:       resc,
	}

	//if pc.client == nil && !pc.isReused() {
	// Hookup the IO
	hyperIo := newHyperIo(pc.conn)
	// We need an executor generally to poll futures
	// Prepare client options
	opts := hyper.NewClientConnOptions()
	opts.Exec(pc.eventLoop.exec)
	// send the handshake
	handshakeTask := hyper.Handshake(hyperIo, opts)
	clientTaskData.taskId = handshake
	handshakeTask.SetUserdata(c.Pointer(clientTaskData), nil)
	// Send the request to readWriteLoop().
	pc.eventLoop.exec.Push(handshakeTask)
	//} else {
	//	println("############### roundTrip: pc.client != nil")
	//	clientTaskData.taskId = read
	//	err = req.write(pc.client, clientTaskData, pc.eventLoop.exec)
	//	if err != nil {
	//		writeErrCh <- err
	//		pc.close(err)
	//	}
	//}

	// Wake up libuv. Loop
	pc.eventLoop.async.Send()

	timeoutch := req.timeoutch
	pcClosed := pc.closech
	canceled := false

	for {
		testHookWaitResLoop()
		if debugSwitch {
			println("############### roundTrip for")
		}
		select {
		case err := <-writeErrCh:
			if debugSwitch {
				println("############### roundTrip: writeErrch")
			}
			if err != nil {
				if debugSwitch {
					println("############### roundTrip: writeErrch err != nil")
				}
				pc.close(fmt.Errorf("write error: %w", err))
				if pc.conn.nwrite == startBytesWritten {
					err = nothingWrittenError{err}
				}
				return nil, pc.mapRoundTripError(req, startBytesWritten, err)
			}
		case <-pcClosed:
			if debugSwitch {
				println("############### roundTrip: pcClosed")
			}
			pcClosed = nil
			if canceled || pc.t.replaceReqCanceler(req.cancelKey, nil) {
				return nil, pc.mapRoundTripError(req, startBytesWritten, pc.closed)
			}
		//case <-respHeaderTimer:
		case re := <-resc:
			if debugSwitch {
				println("############### roundTrip: resc")
			}
			if (re.res == nil) == (re.err == nil) {
				return nil, fmt.Errorf("internal error: exactly one of res or err should be set; nil=%v", re.res == nil)
			}
			if re.err != nil {
				return nil, pc.mapRoundTripError(req, startBytesWritten, re.err)
			}
			return re.res, nil
		case <-timeoutch:
			if debugSwitch {
				println("############### roundTrip: timeoutch")
			}
			canceled = pc.t.cancelRequest(req.cancelKey, errors.New("timeout: req.Context().Err()"))
			timeoutch = nil
			return nil, errors.New("request timeout")
		}
	}
}

// readWriteLoop handles the main I/O loop for a persistent connection.
// It processes incoming requests, sends them to the server, and handles responses.
func readWriteLoop(checker *libuv.Idle) {
	eventLoop := (*clientEventLoop)((*libuv.Handle)(c.Pointer(checker)).GetData())

	// The polling state machine! Poll all ready tasks and act on them...
	task := eventLoop.exec.Poll()
	for task != nil {
		if debugSwitch {
			println("############### polling")
		}
		eventLoop.handleTask(task)
		task = eventLoop.exec.Poll()
	}
}

func (eventLoop *clientEventLoop) handleTask(task *hyper.Task) {
	clientTaskData := (*clientTaskData)(task.Userdata())
	if clientTaskData == nil {
		// A background task for hyper_client completed...
		task.Free()
		return
	}
	var err error
	pc := clientTaskData.pc
	// If original taskId is set, we need to check it
	err = checkTaskType(task, clientTaskData)
	if err != nil {
		if debugSwitch {
			println("############### handleTask: checkTaskType err != nil")
		}
		closeAndRemoveIdleConn(pc, true)
		return
	}
	switch clientTaskData.taskId {
	case handshake:
		if debugReadWriteLoop {
			println("############### write")
		}

		// Check if the connection is closed
		select {
		case <-pc.closech:
			task.Free()
			return
		default:
		}

		pc.client = (*hyper.ClientConn)(task.Value())
		task.Free()

		// TODO(hah) Proxy(writeLoop)
		clientTaskData.taskId = read
		err = clientTaskData.req.Request.write(pc.client, clientTaskData, eventLoop.exec)

		if err != nil {
			//pc.writeErrCh <- err // to the body reader, which might recycle us
			clientTaskData.writeErrCh <- err // to the roundTrip function
			if debugSwitch {
				println("############### handleTask: write err != nil")
			}
			pc.close(err)
			return
		}

		if debugReadWriteLoop {
			println("############### write end")
		}
	case read:
		if debugReadWriteLoop {
			println("############### read")
		}

		pc.tryPutIdleConn = func() bool {
			if err := pc.t.tryPutIdleConn(pc); err != nil {
				pc.closeErr = err
				//if trace != nil && trace.PutIdleConn != nil && err != errKeepAlivesDisabled {
				//	trace.PutIdleConn(err)
				//}
				return false
			}
			//if trace != nil && trace.PutIdleConn != nil {
			//	trace.PutIdleConn(nil)
			//}
			return true
		}

		// Take the results
		hyperResp := (*hyper.Response)(task.Value())
		task.Free()

		//pc.mu.Lock()
		if pc.numExpectedResponses == 0 {
			pc.readLoopPeekFailLocked(hyperResp, err)
			pc.mu.Unlock()
			if debugSwitch {
				println("############### handleTask: numExpectedResponses == 0")
			}
			closeAndRemoveIdleConn(pc, true)
			return
		}
		//pc.mu.Unlock()

		var resp *Response
		if err == nil {
			pc.chunkAsync.SetData(c.Pointer(clientTaskData))
			bc := newBodyStream(pc.chunkAsync)
			pc.bodyStream = bc
			resp, err = ReadResponse(bc, clientTaskData.req.Request, hyperResp)
			clientTaskData.hyperBody = hyperResp.Body()
		} else {
			err = transportReadFromServerError{err}
			pc.closeErr = err
		}

		// No longer need the response
		hyperResp.Free()

		if err != nil {
			pc.bodyStream.closeWithError(err)
			clientTaskData.closeHyperBody()
			select {
			case clientTaskData.resc <- responseAndError{err: err}:
			case <-clientTaskData.callerGone:
				if debugSwitch {
					println("############### handleTask read: callerGone")
				}
				closeAndRemoveIdleConn(pc, true)
				return
			}
			if debugSwitch {
				println("############### handleTask: read err != nil")
			}
			closeAndRemoveIdleConn(pc, true)
			return
		}

		clientTaskData.taskId = readBodyChunk

		if !clientTaskData.req.deadline.IsZero() {
			(*timeoutData)((*libuv.Handle)(c.Pointer(clientTaskData.req.timer)).GetData()).clientTaskData = clientTaskData
		}

		//pc.mu.Lock()
		pc.numExpectedResponses--
		//pc.mu.Unlock()

		needContinue := resp.checkRespBody(clientTaskData)
		if needContinue {
			return
		}

		resp.wrapRespBody(clientTaskData)

		select {
		case clientTaskData.resc <- responseAndError{res: resp}:
		case <-clientTaskData.callerGone:
			// defer
			if debugSwitch {
				println("############### handleTask read: callerGone 2")
			}
			pc.bodyStream.Close()
			clientTaskData.closeHyperBody()
			closeAndRemoveIdleConn(pc, true)
			return
		}

		if debugReadWriteLoop {
			println("############### read end")
		}
	case readBodyChunk:
		if debugReadWriteLoop {
			println("############### readBodyChunk")
		}

		taskType := task.Type()
		if taskType == hyper.TaskBuf {
			chunk := (*hyper.Buf)(task.Value())
			chunkLen := chunk.Len()
			bytes := unsafe.Slice(chunk.Bytes(), chunkLen)
			// Free chunk and task
			chunk.Free()
			task.Free()
			// Write to the channel
			pc.bodyStream.readCh <- bytes
			if debugReadWriteLoop {
				println("############### readBodyChunk end [buf]")
			}
			return
		}

		// taskType == taskEmpty (check in checkTaskType)
		task.Free()
		pc.bodyStream.closeWithError(io.EOF)
		clientTaskData.closeHyperBody()
		replaced := pc.t.replaceReqCanceler(clientTaskData.req.cancelKey, nil) // before pc might return to idle pool
		pc.alive = pc.alive &&
			replaced && pc.tryPutIdleConn()

		if debugSwitch {
			println("############### handleTask readBodyChunk: alive: ", pc.alive)
		}
		closeAndRemoveIdleConn(pc, false)

		if debugReadWriteLoop {
			println("############### readBodyChunk end [empty]")
		}
	}
}

func readyToRead(aysnc *libuv.Async) {
	clientTaskData := (*clientTaskData)(aysnc.GetData())
	dataTask := clientTaskData.hyperBody.Data()
	dataTask.SetUserdata(c.Pointer(clientTaskData), nil)
	clientTaskData.pc.eventLoop.exec.Push(dataTask)
}

// closeAndRemoveIdleConn Replace the defer function of readLoop in stdlib
func closeAndRemoveIdleConn(pc *persistConn, force bool) {
	if pc.alive == true && !force {
		return
	}
	if debugSwitch {
		println("############### closeAndRemoveIdleConn, force:", force)
	}
	pc.close(pc.closeErr)
	pc.t.removeIdleConn(pc)
}

// ----------------------------------------------------------

type connData struct {
	tcpHandle     libuv.Tcp
	connectReq    libuv.Connect
	readBuf       libuv.Buf
	readBufFilled uintptr
	nwrite        int64 // bytes written(Replaced from persistConn's nwrite)
	readWaker     *hyper.Waker
	writeWaker    *hyper.Waker
	isClosing     atomic.Bool
}

type clientTaskData struct {
	taskId     taskId
	req        *transportRequest
	pc         *persistConn
	addedGzip  bool
	writeErrCh chan error
	callerGone chan struct{}
	resc       chan responseAndError
	hyperBody  *hyper.Body
}

// taskId The unique identifier of the next task polled from the executor
type taskId c.Int

const (
	handshake taskId = iota + 1
	read
	readBodyChunk
)

func (conn *connData) Close() {
	if conn != nil && !conn.isClosing.Swap(true) {
		if conn.readWaker != nil {
			conn.readWaker.Free()
			conn.readWaker = nil
		}
		if conn.writeWaker != nil {
			conn.writeWaker.Free()
			conn.writeWaker = nil
		}
		//if conn.readBuf.Base != nil {
		//	c.Free(c.Pointer(conn.readBuf.Base))
		//	conn.readBuf.Base = nil
		//}
		if (*libuv.Handle)(c.Pointer(&conn.tcpHandle)).IsClosing() == 0 {
			(*libuv.Handle)(c.Pointer(&conn.tcpHandle)).Close(nil)
		}
		conn = nil
	}
}

func (d *clientTaskData) closeHyperBody() {
	if d.hyperBody != nil {
		d.hyperBody.Free()
		d.hyperBody = nil
	}
}

// onConnect is the libuv callback for a successful connection
func onConnect(req *libuv.Connect, status c.Int) {
	if debugSwitch {
		println("############### connect start")
		defer println("############### connect end")
	}
	conn := (*connData)((*libuv.Req)(c.Pointer(req)).GetData())
	if status < 0 {
		c.Fprintf(c.Stderr, c.Str("connect error: %s\n"), c.GoString(libuv.Strerror(libuv.Errno(status))))
		conn.Close()
		return
	}

	// Keep-Alive
	conn.tcpHandle.KeepAlive(1, 60)

	(*libuv.Stream)(c.Pointer(&conn.tcpHandle)).StartRead(allocBuffer, onRead)
}

// allocBuffer allocates a buffer for reading from a socket
func allocBuffer(handle *libuv.Handle, suggestedSize uintptr, buf *libuv.Buf) {
	conn := (*connData)(handle.GetData())
	if conn.readBuf.Base == nil {
		//conn.readBuf = libuv.InitBuf((*c.Char)(c.Malloc(suggestedSize)), c.Uint(suggestedSize))
		base := make([]byte, suggestedSize)
		conn.readBuf = libuv.InitBuf((*c.Char)(c.Pointer(&base[0])), c.Uint(suggestedSize))
		conn.readBufFilled = 0
	}
	*buf = libuv.InitBuf((*c.Char)(c.Pointer(uintptr(c.Pointer(conn.readBuf.Base))+conn.readBufFilled)), c.Uint(suggestedSize-conn.readBufFilled))
}

// onRead is the libuv callback for reading from a socket
// This callback function is called when data is available to be read
func onRead(stream *libuv.Stream, nread c.Long, buf *libuv.Buf) {
	conn := (*connData)((*libuv.Handle)(c.Pointer(stream)).GetData())
	if nread > 0 {
		conn.readBufFilled += uintptr(nread)
	}
	if conn.readWaker != nil {
		// Wake up the pending read operation of Hyper
		conn.readWaker.Wake()
		conn.readWaker = nil
	}
}

// readCallBack read callback function for Hyper library
func readCallBack(userdata c.Pointer, ctx *hyper.Context, buf *uint8, bufLen uintptr) uintptr {
	conn := (*connData)(userdata)
	if conn.readBufFilled > 0 {
		var toCopy uintptr
		if bufLen < conn.readBufFilled {
			toCopy = bufLen
		} else {
			toCopy = conn.readBufFilled
		}
		// Copy data from read buffer to Hyper's buffer
		c.Memcpy(c.Pointer(buf), c.Pointer(conn.readBuf.Base), toCopy)
		// Move remaining data to the beginning of the buffer
		c.Memmove(c.Pointer(conn.readBuf.Base), c.Pointer(uintptr(c.Pointer(conn.readBuf.Base))+toCopy), conn.readBufFilled-toCopy)
		// Update the amount of filled buffer
		conn.readBufFilled -= toCopy
		return toCopy
	}

	if conn.readWaker != nil {
		conn.readWaker.Free()
	}
	conn.readWaker = ctx.Waker()
	println("############### readCallBack: IoPending")
	return hyper.IoPending
}

// onWrite is the libuv callback for writing to a socket
// Callback function called after a write operation completes
func onWrite(req *libuv.Write, status c.Int) {
	conn := (*connData)((*libuv.Req)(c.Pointer(req)).GetData())
	if conn.writeWaker != nil {
		// Wake up the pending write operation
		conn.writeWaker.Wake()
		conn.writeWaker = nil
	}
}

// writeCallBack write callback function for Hyper library
func writeCallBack(userdata c.Pointer, ctx *hyper.Context, buf *uint8, bufLen uintptr) uintptr {
	conn := (*connData)(userdata)
	initBuf := libuv.InitBuf((*c.Char)(c.Pointer(buf)), c.Uint(bufLen))
	req := &libuv.Write{}
	(*libuv.Req)(c.Pointer(req)).SetData(c.Pointer(conn))

	ret := req.Write((*libuv.Stream)(c.Pointer(&conn.tcpHandle)), &initBuf, 1, onWrite)
	if ret >= 0 {
		conn.nwrite += int64(bufLen)
		return bufLen
	}

	if conn.writeWaker != nil {
		conn.writeWaker.Free()
	}
	conn.writeWaker = ctx.Waker()
	println("############### writeCallBack: IoPending")
	return hyper.IoPending
}

// onTimeout is the libuv callback for a timeout
func onTimeout(timer *libuv.Timer) {
	if debugSwitch {
		println("############### onTimeout start")
		defer println("############### onTimeout end")
	}
	data := (*timeoutData)((*libuv.Handle)(c.Pointer(timer)).GetData())
	close(data.timeoutch)
	timer.Stop()

	clientTaskData := data.clientTaskData
	if clientTaskData != nil {
		pc := clientTaskData.pc
		pc.alive = false
		pc.t.cancelRequest(clientTaskData.req.cancelKey, errors.New("timeout: req.Context().Err()"))
		closeAndRemoveIdleConn(pc, true)
	}
}

// newHyperIo creates a new IO with read and write callbacks
func newHyperIo(connData *connData) *hyper.Io {
	hyperIo := hyper.NewIo()
	hyperIo.SetUserdata(c.Pointer(connData), nil)
	hyperIo.SetRead(readCallBack)
	hyperIo.SetWrite(writeCallBack)
	return hyperIo
}

// checkTaskType checks the task type
func checkTaskType(task *hyper.Task, clientTaskData *clientTaskData) (err error) {
	curTaskId := clientTaskData.taskId
	taskType := task.Type()
	if taskType == hyper.TaskError {
		err = fail((*hyper.Error)(task.Value()), curTaskId)
	}
	if err == nil {
		switch curTaskId {
		case handshake:
			if taskType != hyper.TaskClientConn {
				err = errors.New("Unexpected hyper task type: expected to be TaskClientConn, actual is " + strTaskType(taskType))
			}
		case read:
			if taskType != hyper.TaskResponse {
				err = errors.New("Unexpected hyper task type: expected to be TaskResponse, actual is " + strTaskType(taskType))
			}
		case readBodyChunk:
			if taskType != hyper.TaskBuf && taskType != hyper.TaskEmpty {
				err = errors.New("Unexpected hyper task type: expected to be TaskBuf / TaskEmpty, actual is " + strTaskType(taskType))
			}
		}
	}
	if err != nil {
		task.Free()
		if curTaskId == handshake || curTaskId == read {
			clientTaskData.writeErrCh <- err
			if debugSwitch {
				println("############### checkTaskType: writeErrCh")
			}
			clientTaskData.pc.close(err)
		}
		if clientTaskData.pc.bodyStream != nil {
			clientTaskData.pc.bodyStream.Close()
			clientTaskData.pc.bodyStream = nil
		}
		clientTaskData.closeHyperBody()
		clientTaskData.pc.alive = false
	}
	return
}

// fail prints the error details and panics
func fail(err *hyper.Error, taskId taskId) error {
	if err != nil {
		// grab the error details
		var errBuf [256]c.Char
		errLen := err.Print((*uint8)(c.Pointer(&errBuf[:][0])), uintptr(len(errBuf)))
		errDetails := unsafe.SliceData(errBuf[:errLen])
		details := c.GoString(errDetails)
		fmt.Println(details)

		// clean up the error
		err.Free()
		return fmt.Errorf("hyper request error, taskId: %s, details: %s\n", strTaskId(taskId), details)
	}
	return nil
}

func strTaskType(taskType hyper.TaskReturnType) string {
	switch taskType {
	case hyper.TaskClientConn:
		return "TaskClientConn"
	case hyper.TaskResponse:
		return "TaskResponse"
	case hyper.TaskBuf:
		return "TaskBuf"
	case hyper.TaskEmpty:
		return "TaskEmpty"
	case hyper.TaskError:
		return "TaskError"
	default:
		return "Unknown"
	}
}

func strTaskId(taskId taskId) string {
	switch taskId {
	case handshake:
		return "handshake"
	case read:
		return "read"
	case readBodyChunk:
		return "readBodyChunk"
	default:
		return "notSet"
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
	errReadLoopExiting    = errors.New("http: Transport.readWriteLoop.read exiting")
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

	t         *Transport
	eventLoop *clientEventLoop

	cacheKey connectMethodKey
	conn     *connData
	//tlsState *tls.ConnectionState
	//nwrite        int64       // bytes written(Replaced by connData.nwrite)
	closech chan struct{} // closed when conn closed
	isProxy bool

	writeLoopDone chan struct{} // closed when readWriteLoop ends

	// Both guarded by Transport.idleMu:
	idleAt    time.Time    // time it last become idle
	idleTimer *libuv.Timer // holding an onIdleConnTimeout to close it

	mu                   sync.Mutex // guards following fields
	numExpectedResponses int
	closed               error // set non-nil when conn is closed, before closech is closed
	canceledErr          error // set non-nil if conn is canceled
	broken               bool  // an error has happened on this connection; marked broken so it's not reused.
	// mutateHeaderFunc is an optional func to modify extra
	// headers on each outbound request before it's written. (the
	// original Request given to RoundTrip is not modified)
	reused           bool // whether conn has had successful request/response and is being reused.
	mutateHeaderFunc func(Header)

	// other
	alive          bool              // Replace the alive in readLoop
	closeErr       error             // Replace the closeErr in readLoop
	tryPutIdleConn func() bool       // Replace the tryPutIdleConn in readLoop
	client         *hyper.ClientConn // http long connection client handle
	bodyStream     *bodyStream        // Implement non-blocking consumption of each responseBody chunk
	chunkAsync     *libuv.Async      // Notifying that the received chunk has been read
}

// CloseIdleConnections closes any connections which were previously
// connected from previous requests but are now sitting idle in
// a "keep-alive" state. It does not interrupt any connections currently
// in use.
func (t *Transport) CloseIdleConnections() {
	if debugSwitch {
		println("############### CloseIdleConnections")
	}
	//t.nextProtoOnce.Do(t.onceSetNextProtoDefaults)
	t.idleMu.Lock()
	m := t.idleConn
	t.idleConn = nil
	t.closeIdle = true // close newly idle connections
	t.idleLRU = connLRU{}
	t.idleMu.Unlock()
	for _, conns := range m {
		for _, pconn := range conns {
			pconn.close(errCloseIdleConns)
		}
	}

	//if t2 := t.h2transport; t2 != nil {
	//	t2.CloseIdleConnections()
	//}
}

func (pc *persistConn) cancelRequest(err error) {
	if debugSwitch {
		println("############### cancelRequest")
	}
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

// markReused marks this connection as having been successfully used for a
// request and response.
func (pc *persistConn) markReused() {
	pc.mu.Lock()
	pc.reused = true
	pc.mu.Unlock()
}

func (pc *persistConn) closeLocked(err error) {
	if debugSwitch {
		println("############### pc closed")
	}
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
			if pc.client != nil {
				pc.client.Free()
				pc.client = nil
			}
			if pc.chunkAsync != nil && pc.chunkAsync.IsClosing() == 0 {
				pc.chunkAsync.Close(nil)
				pc.chunkAsync = nil
			}
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

// isReused reports whether this connection has been used before.
func (pc *persistConn) isReused() bool {
	pc.mu.Lock()
	r := pc.reused
	pc.mu.Unlock()
	return r
}

// isBroken reports whether this connection is in a known broken state.
func (pc *persistConn) isBroken() bool {
	pc.mu.Lock()
	b := pc.closed != nil
	pc.mu.Unlock()
	return b
}

// shouldRetryRequest reports whether we should retry sending a failed
// HTTP request on a new connection. The non-nil input error is the
// error from roundTrip.
func (pc *persistConn) shouldRetryRequest(req *Request, err error) bool {
	if http2isNoCachedConnError(err) {
		// Issue 16582: if the user started a bunch of
		// requests at once, they can all pick the same conn
		// and violate the server's max concurrent streams.
		// Instead, match the HTTP/1 behavior for now and dial
		// again to get a new TCP connection, rather than failing
		// this request.
		return true
	}
	if err == errMissingHost {
		// User error.
		return false
	}
	if !pc.isReused() {
		// This was a fresh connection. There's no reason the server
		// should've hung up on us.
		//
		// Also, if we retried now, we could loop forever
		// creating new connections and retrying if the server
		// is just hanging up on us because it doesn't like
		// our request (as opposed to sending an error).
		return false
	}
	if _, ok := err.(nothingWrittenError); ok {
		// We never wrote anything, so it's safe to retry, if there's no body or we
		// can "rewind" the body with GetBody.
		return req.outgoingLength() == 0 || req.GetBody != nil
	}
	if !req.isReplayable() {
		// Don't retry non-idempotent requests.
		return false
	}
	if _, ok := err.(transportReadFromServerError); ok {
		// We got some non-EOF net.Conn.Read failure reading
		// the 1st response byte from the server.
		return true
	}
	// The server replied with io.EOF while we were trying to
	// read the response. Probably an unfortunately keep-alive
	// timeout, just as the client was writing a request.
	// conservatively return false.
	return err == errServerClosedIdle
}

// closeConnIfStillIdle closes the connection if it's still sitting idle.
// This is what's called by the persistConn's idleTimer, and is run in its
// own goroutine.
func (pc *persistConn) closeConnIfStillIdle() bool {
	t := pc.t
	isLock := t.idleMu.TryLock()
	if isLock {
		defer t.idleMu.Unlock()
		pc.closeConnIfStillIdleLocked()
		return true
	}
	return false
}

func (pc *persistConn) closeConnIfStillIdleLocked() {
	t := pc.t
	if _, ok := t.idleLRU.m[pc]; !ok {
		// Not idle.
		return
	}
	t.removeIdleConnLocked(pc)
	if debugSwitch {
		println("############### closeConnIfStillIdleLocked")
	}
	pc.close(errIdleConnTimeout)
}

func (pc *persistConn) readLoopPeekFailLocked(resp *hyper.Response, err error) {
	if debugSwitch {
		println("############### readLoopPeekFailLocked")
	}
	if pc.closed != nil {
		return
	}
	if is408Message(resp) {
		pc.closeLocked(errServerClosedIdle)
		return
	}
	pc.closeLocked(fmt.Errorf("readLoopPeekFailLocked: %w", err))
}

// setExtraHeaders Set extra headers, such as Accept-Encoding, Connection(Keep-Alive).
func (pc *persistConn) setExtraHeaders(req *transportRequest) bool {
	// Ask for a compressed version if the caller didn't set their
	// own value for Accept-Encoding. We only attempt to
	// uncompress the gzip stream if we were the layer that
	// requested it.
	requestedGzip := false
	// TODO(hah) gzip(pc.roundTrip): The compress/gzip library still has a bug. An exception occurs when calling gzip.NewReader().
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
	return requestedGzip
}

func is408Message(resp *hyper.Response) bool {
	httpVersion := int(resp.Version())
	if httpVersion != 10 && httpVersion != 11 {
		return false
	}
	return resp.Status() == 408
}

// isNoCachedConnError reports whether err is of type noCachedConnError
// or its equivalent renamed type in net/http2's h2_bundle.go. Both types
// may coexist in the same running program.
func http2isNoCachedConnError(err error) bool { // h2_bundle.go
	_, ok := err.(interface{ IsHTTP2NoCachedConnError() })
	return ok
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

	eventLoop *clientEventLoop
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
	ready     bool
	//ready     chan struct{}    // closed when pc, err pair is delivered

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
		w.ready = true // catch misbehavior in future delivery
	}
	pc := w.pc
	w.pc = nil
	w.err = err
	w.mu.Unlock()

	if pc != nil {
		t.putOrCloseIdleConn(pc)
	}
}

// waiting reports whether w is still waiting for an answer (connection or error).
func (w *wantConn) waiting() bool {
	if w.ready {
		return false
	} else {
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
	w.ready = true
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

type connLRU struct {
	ll *list.List // list.Element.Value type is of *persistConn
	m  map[*persistConn]*list.Element
}

// add adds pc to the head of the linked list.
func (cl *connLRU) add(pc *persistConn) {
	if cl.ll == nil {
		cl.ll = list.New()
		cl.m = make(map[*persistConn]*list.Element)
	}
	ele := cl.ll.PushFront(pc)
	if _, ok := cl.m[pc]; ok {
		panic("persistConn was already in LRU")
	}
	cl.m[pc] = ele
}

func (cl *connLRU) removeOldest() *persistConn {
	ele := cl.ll.Back()
	pc := ele.Value.(*persistConn)
	cl.ll.Remove(ele)
	delete(cl.m, pc)
	return pc
}

// remove removes pc from cl.
func (cl *connLRU) remove(pc *persistConn) {
	if ele, ok := cl.m[pc]; ok {
		cl.ll.Remove(ele)
		delete(cl.m, pc)
	}
}

// len returns the number of items in the cache.
func (cl *connLRU) len() int {
	return len(cl.m)
}
