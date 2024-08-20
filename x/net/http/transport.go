package http

import (
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
	"github.com/goplus/llgoexamples/rust/hyper"
)

type connData struct {
	TcpHandle     libuv.Tcp
	ConnectReq    libuv.Connect
	ReadBuf       libuv.Buf
	TimeoutTimer  libuv.Timer
	IsCompleted   int
	ReadBufFilled uintptr
	ReadWaker     *hyper.Waker
	WriteWaker    *hyper.Waker
}

type Transport struct {
	altProto    atomic.Value // of nil or map[string]RoundTripper, key is URI scheme
	reqMu       sync.Mutex
	reqCanceler map[cancelKey]func(error)
	//Proxy       func(*Request) (*url.URL, error)

	// MaxConnsPerHost optionally limits the total number of
	// connections per host, including connections in the dialing,
	// active, and idle states. On limit violation, dials will block.
	//
	// Zero means no limit.
	MaxConnsPerHost int
}

var DefaultTransport RoundTripper = &Transport{}

// taskId The unique identifier of the next task polled from the executor
type taskId c.Int

const (
	notSet taskId = iota
	sending
	receiveResp
	receiveRespBody
)

const (
	defaultHTTPPort = "80"
)

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
	conn      *connData
	t         *Transport
	reqch     chan requestAndChan // written by roundTrip; read by readLoop
	cancelch  chan freeChan
	timeoutch chan struct{}
}

// incomparable is a zero-width, non-comparable type. Adding it to a struct
// makes that struct also non-comparable, and generally doesn't add
// any size (as long as it's first).
type incomparable [0]func()

type requestAndChan struct {
	_   incomparable
	req *Request
	ch  chan responseAndError // unbuffered; always send in select on callerGone
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

type freeChan struct {
	_      incomparable
	freech chan struct{}
}

// A cancelKey is the key of the reqCanceler map.
// We wrap the *Request in this type since we want to use the original request,
// not any transient one created by roundTrip.
type cancelKey struct {
	req *Request
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
	//case <-req.Cancel:
	//	return nil, errRequestCanceledConn
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

	go t.dialConnFor(w)
	// TODO(spongehah) MaxConnsPerHost
	//if t.MaxConnsPerHost <= 0 {
	//	go t.dialConnFor(w)
	//	return
	//}

	//t.connsPerHostMu.Lock()
	//defer t.connsPerHostMu.Unlock()
	//
	//if n := t.connsPerHost[w.key]; n < t.MaxConnsPerHost {
	//	if t.connsPerHost == nil {
	//		t.connsPerHost = make(map[connectMethodKey]int)
	//	}
	//	t.connsPerHost[w.key] = n + 1
	//	go t.dialConnFor(w)
	//	return
	//}
	//
	//if t.connsPerHostWait == nil {
	//	t.connsPerHostWait = make(map[connectMethodKey]wantConnQueue)
	//}
	//q := t.connsPerHostWait[w.key]
	//q.cleanFront()
	//q.pushBack(w)
	//t.connsPerHostWait[w.key] = q
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
	//if err == nil && (!delivered || pc.alt != nil) {
	//	// pconn was not passed to w,
	//	// or it is HTTP/2 and can be shared.
	//	// Add to the idle connection pool.
	//	t.putOrCloseIdleConn(pc)
	//}
	//if err != nil {
	//	t.decConnsPerHost(w.key)
	//}
}

func (t *Transport) dialConn(ctx context.Context, cm connectMethod) (pconn *persistConn, err error) {
	pconn = &persistConn{
		t:         t,
		reqch:     make(chan requestAndChan, 1),
		cancelch:  make(chan freeChan, 1),
		timeoutch: make(chan struct{}, 1),
		//writech: make(chan writeRequest, 1),
		//closech: make(chan struct{}),
	}

	// TODO(spongehah) Proxy dialConn

	treq := cm.treq
	host := treq.URL.Hostname()
	port := treq.URL.Port()
	if port == "" {
		// Hyper only supports http
		port = defaultHTTPPort
	}
	loop := libuv.DefaultLoop()
	conn := new(connData)
	pconn.conn = conn
	if conn == nil {
		return nil, fmt.Errorf("Failed to allocate memory for conn_data\n")
	}

	// If timeout is set, start the timer
	if treq.timeout > 0 {
		libuv.InitTimer(loop, &conn.TimeoutTimer)
		ct := &connAndTimeoutChan{
			conn:      conn,
			timeoutch: pconn.timeoutch,
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
		close(pconn.timeoutch)
		return nil, fmt.Errorf("getaddrinfo error\n")
	}

	(*libuv.Req)(c.Pointer(&conn.ConnectReq)).SetData(c.Pointer(conn))
	status = libuv.TcpConnect(&conn.ConnectReq, &conn.TcpHandle, res.Addr, onConnect)
	if status != 0 {
		close(pconn.timeoutch)
		return nil, fmt.Errorf("connect error: %s\n", c.GoString(libuv.Strerror(libuv.Errno(status))))
	}

	net.Freeaddrinfo(res)

	if pconn.conn.IsCompleted != 1 {
		go pconn.readWriteLoop(loop)
	}
	return pconn, nil
}

func (pc *persistConn) roundTrip(req *transportRequest) (*Response, error) {
	testHookEnterRoundTrip()
	resc := make(chan responseAndError, 1)

	pc.reqch <- requestAndChan{
		req: req.Request,
		ch:  resc,
	}
	// Determine whether timeout has occurred
	if pc.conn.IsCompleted == 1 {
		rc := <-pc.reqch // blocking
		// Free the resources
		freeResources(nil, nil, nil, nil, pc, rc)
		return nil, fmt.Errorf("request timeout\n")
	}
	select {
	case re := <-resc:
		if (re.res == nil) == (re.err == nil) {
			return nil, fmt.Errorf("internal error: exactly one of res or err should be set; nil=%v", re.res == nil)
		}
		if re.err != nil {
			return nil, re.err
		}
		return re.res, nil
	case <-pc.timeoutch:
		freech := make(chan struct{}, 1)
		pc.cancelch <- freeChan{
			freech: freech,
		}
		<-freech
		return nil, fmt.Errorf("request timeout\n")
	}
}

// readWriteLoop handles the main I/O loop for a persistent connection.
// It processes incoming requests, sends them to the server, and handles responses.
func (pc *persistConn) readWriteLoop(loop *libuv.Loop) {
	// Hookup the IO
	hyperIo := newIoWithConnReadWrite(pc.conn)

	// We need an executor generally to poll futures
	exec := hyper.NewExecutor()
	// Prepare client options
	opts := hyper.NewClientConnOptions()
	opts.Exec(exec)

	handshakeTask := hyper.Handshake(hyperIo, opts)
	setTaskId(handshakeTask, sending)

	// Let's wait for the handshake to finish...
	exec.Push(handshakeTask)

	// The polling state machine!
	//for {
	// Poll all ready tasks and act on them...
	rc := <-pc.reqch // blocking
	alive := true
	var bodyWriter *io.PipeWriter
	var respBody *hyper.Body = nil
	for alive {
		select {
		case fc := <-pc.cancelch:
			// Free the resources
			freeResources(nil, respBody, bodyWriter, exec, pc, rc)
			alive = false
			close(fc.freech)
			return
		default:
			task := exec.Poll()
			if task == nil {
				loop.Run(libuv.RUN_ONCE)
				continue
			}
			switch (taskId)(uintptr(task.Userdata())) {
			case sending:
				err := checkTaskType(task, sending)
				if err != nil {
					rc.ch <- responseAndError{err: err}
					// Free the resources
					freeResources(task, respBody, bodyWriter, exec, pc, rc)
					return
				}

				client := (*hyper.ClientConn)(task.Value())
				task.Free()

				// Prepare the hyper.Request
				hyperReq, err := newHyperRequest(rc.req)
				if err != nil {
					rc.ch <- responseAndError{err: err}
					// Free the resources
					freeResources(task, respBody, bodyWriter, exec, pc, rc)
					return
				}

				// Send it!
				sendTask := client.Send(hyperReq)
				setTaskId(sendTask, receiveResp)
				sendRes := exec.Push(sendTask)
				if sendRes != hyper.OK {
					rc.ch <- responseAndError{err: fmt.Errorf("failed to send request")}
					// Free the resources
					freeResources(task, respBody, bodyWriter, exec, pc, rc)
					return
				}

				// For this example, no longer need the client
				client.Free()
			case receiveResp:
				err := checkTaskType(task, receiveResp)
				if err != nil {
					rc.ch <- responseAndError{err: err}
					// Free the resources
					freeResources(task, respBody, bodyWriter, exec, pc, rc)
					return
				}

				// Take the results
				hyperResp := (*hyper.Response)(task.Value())
				task.Free()

				resp, err := ReadResponse(hyperResp, rc.req)
				if err != nil {
					rc.ch <- responseAndError{err: err}
					// Free the resources
					freeResources(task, respBody, bodyWriter, exec, pc, rc)
					return
				}

				respBody = hyperResp.Body()
				resp.Body, bodyWriter = io.Pipe()

				rc.ch <- responseAndError{res: resp}

				// Response has been returned, stop the timer
				pc.conn.IsCompleted = 1
				// Stop the timer
				if rc.req.timeout > 0 {
					pc.conn.TimeoutTimer.Stop()
					(*libuv.Handle)(c.Pointer(&pc.conn.TimeoutTimer)).Close(nil)
				}

				dataTask := respBody.Data()
				setTaskId(dataTask, receiveRespBody)
				exec.Push(dataTask)

				// No longer need the response
				hyperResp.Free()
			case receiveRespBody:
				err := checkTaskType(task, receiveRespBody)
				if err != nil {
					rc.ch <- responseAndError{err: err}
					// Free the resources
					freeResources(task, respBody, bodyWriter, exec, pc, rc)
					return
				}

				if task.Type() == hyper.TaskBuf {
					buf := (*hyper.Buf)(task.Value())
					bufLen := buf.Len()
					bytes := unsafe.Slice((*byte)(buf.Bytes()), bufLen)
					if bodyWriter == nil {
						rc.ch <- responseAndError{err: fmt.Errorf("ResponseBodyWriter is nil")}
						// Free the resources
						freeResources(task, respBody, bodyWriter, exec, pc, rc)
						return
					}
					_, err := bodyWriter.Write(bytes) // blocking
					if err != nil {
						rc.ch <- responseAndError{err: err}
						// Free the resources
						freeResources(task, respBody, bodyWriter, exec, pc, rc)
						return
					}
					buf.Free()
					task.Free()

					dataTask := respBody.Data()
					setTaskId(dataTask, receiveRespBody)
					exec.Push(dataTask)

					break
				}

				// We are done with the response body
				if task.Type() != hyper.TaskEmpty {
					c.Printf(c.Str("unexpected task type\n"))
					rc.ch <- responseAndError{err: fmt.Errorf("unexpected task type\n")}
					// Free the resources
					freeResources(task, respBody, bodyWriter, exec, pc, rc)
					return
				}

				// Free the resources
				freeResources(task, respBody, bodyWriter, exec, pc, rc)

				alive = false
			case notSet:
				// A background task for hyper_client completed...
				task.Free()
			}
		}
	}
	//}
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
	//conn := (*ConnData)(stream.Data)
	//conn := (*struct{ data *ConnData })(c.Pointer(stream)).data

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
	//conn := (*ConnData)(req.Data)
	//conn := (*struct{ data *ConnData })(c.Pointer(req)).data

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
	//req := (*libuv.Write)(c.Malloc(unsafe.Sizeof(libuv.Write{})))
	req := &libuv.Write{}
	// Associate the connection data with the write request
	(*libuv.Req)(c.Pointer(req)).SetData(c.Pointer(conn))
	//req.Data = c.Pointer(conn)

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
	if ct.conn.IsCompleted != 1 {
		ct.conn.IsCompleted = 1
		ct.timeoutch <- struct{}{}
	}
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

// setTaskId Set taskId to the task's userdata as a unique identifier
func setTaskId(task *hyper.Task, userData taskId) {
	var data = userData
	task.SetUserdata(unsafe.Pointer(uintptr(data)))
}

// checkTaskType checks the task type
func checkTaskType(task *hyper.Task, curTaskId taskId) error {
	switch curTaskId {
	case sending:
		if task.Type() == hyper.TaskError {
			c.Printf(c.Str("handshake task error!\n"))
			return fail((*hyper.Error)(task.Value()))
		}
		if task.Type() != hyper.TaskClientConn {
			return fmt.Errorf("unexpected task type\n")
		}
		return nil
	case receiveResp:
		if task.Type() == hyper.TaskError {
			c.Printf(c.Str("send task error!\n"))
			return fail((*hyper.Error)(task.Value()))
		}
		if task.Type() != hyper.TaskResponse {
			c.Printf(c.Str("unexpected task type\n"))
			return fmt.Errorf("unexpected task type\n")
		}
		return nil
	case receiveRespBody:
		if task.Type() == hyper.TaskError {
			c.Printf(c.Str("body error!\n"))
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
	close(pc.timeoutch)
	close(pc.cancelch)
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

type httpError struct {
	err     string
	timeout bool
}

func (e *httpError) Error() string   { return e.err }
func (e *httpError) Timeout() bool   { return e.timeout }
func (e *httpError) Temporary() bool { return true }

func nop() {}

// ErrSkipAltProtocol is a sentinel error value defined by Transport.RegisterProtocol.
var ErrSkipAltProtocol = errors.New("net/http: skip alternate protocol")

var errCannotRewind = errors.New("net/http: cannot rewind body after connection loss")

var errTimeout error = &httpError{err: "net/http: timeout awaiting response headers", timeout: true}

// errRequestCanceled is set to be identical to the one from h2 to facilitate
// testing.
var errRequestCanceled = http2errRequestCanceled

// errRequestCanceled is a copy of net/http's errRequestCanceled because it's not
// exported. At least they'll be DeepEqual for h1-vs-h2 comparisons tests.
var http2errRequestCanceled = errors.New("net/http: request canceled")
var errRequestCanceledConn = errors.New("net/http: request canceled while waiting for connection") // TODO: unify?

/*// alternateRoundTripper returns the alternate RoundTripper to use
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
*/

func idnaASCIIFromURL(url *url.URL) string {
	addr := url.Hostname()
	if v, err := idnaASCII(addr); err == nil {
		addr = v
	}
	return addr
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

// fakeLocker is a sync.Locker which does nothing. It's used to guard
// test-only fields when not under test, to avoid runtime atomic
// overhead.
type fakeLocker struct{}

func (fakeLocker) Lock()   {}
func (fakeLocker) Unlock() {}

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

func (t *Transport) connectMethodForRequest(treq *transportRequest) (cm connectMethod, err error) {
	cm.targetScheme = treq.URL.Scheme
	// TODO(spongehah) canonicalAddr & Proxy
	//cm.targetAddr = canonicalAddr(treq.URL)
	//if t.Proxy != nil {
	//	cm.proxyURL, err = t.Proxy(treq.Request)
	//}
	cm.treq = treq
	cm.onlyH1 = treq.requiresHTTP1()
	return cm, err
}

// connectMethodKey is the map key version of connectMethod, with a
// stringified proxy URL (or the empty string) instead of a pointer to
// a URL.
type connectMethodKey struct {
	proxy, scheme, addr string
	onlyH1              bool
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
