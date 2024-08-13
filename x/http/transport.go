package http

import (
	"fmt"
	"io"
	"unsafe"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/libuv"
	"github.com/goplus/llgo/c/net"
	"github.com/goplus/llgo/c/syscall"
	"github.com/goplus/llgoexamples/rust/hyper"
)

type ConnData struct {
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
}

// TaskId The unique identifier of the next task polled from the executor
type TaskId c.Int

const (
	NotSet TaskId = iota
	Send
	ReceiveResp
	ReceiveRespBody
)

const (
	DefaultHTTPPort = "80"
)

var DefaultTransport RoundTripper = &Transport{}

// persistConn wraps a connection, usually a persistent one
// (but may be used for non-keep-alive requests as well)
type persistConn struct {
	// alt optionally specifies the TLS NextProto RoundTripper.
	// This is used for HTTP/2 today and future protocols later.
	// If it's non-nil, the rest of the fields are unused.
	//alt  RoundTripper
	//br      *bufio.Reader       // from conn
	//bw      *bufio.Writer       // to conn
	//nwrite  int64               // bytes written
	//writech chan writeRequest   // written by roundTrip; read by writeLoop
	//closech chan struct{}       // closed when conn closed
	conn      *ConnData
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
	conn      *ConnData
	timeoutch chan struct{}
}

type freeChan struct {
	_      incomparable
	freech chan struct{}
}

func (t *Transport) RoundTrip(req *Request) (*Response, error) {
	pconn, err := t.getConn(req)
	if err != nil {
		return nil, err
	}
	var resp *Response
	resp, err = pconn.roundTrip(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (t *Transport) getConn(req *Request) (pconn *persistConn, err error) {
	host := req.Host
	port := req.URL.Port()
	if port == "" {
		// Hyper only supports http
		port = DefaultHTTPPort
	}
	loop := libuv.DefaultLoop()
	//conn := (*ConnData)(c.Calloc(1, unsafe.Sizeof(ConnData{})))
	conn := new(ConnData)
	if conn == nil {
		return nil, fmt.Errorf("Failed to allocate memory for conn_data\n")
	}

	// If timeout is set, start the timer
	timeoutch := make(chan struct{}, 1)
	if req.timeout != 0 {
		libuv.InitTimer(loop, &conn.TimeoutTimer)
		ct := &connAndTimeoutChan{
			conn:      conn,
			timeoutch: timeoutch,
		}
		(*libuv.Handle)(c.Pointer(&conn.TimeoutTimer)).SetData(c.Pointer(ct))
		conn.TimeoutTimer.Start(OnTimeout, uint64(req.timeout.Milliseconds()), 0)
	}

	libuv.InitTcp(loop, &conn.TcpHandle)
	//conn.TcpHandle.Data = c.Pointer(conn)
	(*libuv.Handle)(c.Pointer(&conn.TcpHandle)).SetData(c.Pointer(conn))

	var hints net.AddrInfo
	c.Memset(c.Pointer(&hints), 0, unsafe.Sizeof(hints))
	hints.Family = syscall.AF_UNSPEC
	hints.SockType = syscall.SOCK_STREAM

	var res *net.AddrInfo
	status := net.Getaddrinfo(c.AllocaCStr(host), c.AllocaCStr(port), &hints, &res)
	if status != 0 {
		close(timeoutch)
		return nil, fmt.Errorf("getaddrinfo error\n")
	}

	//conn.ConnectReq.Data = c.Pointer(conn)
	(*libuv.Req)(c.Pointer(&conn.ConnectReq)).SetData(c.Pointer(conn))
	status = libuv.TcpConnect(&conn.ConnectReq, &conn.TcpHandle, res.Addr, OnConnect)
	if status != 0 {
		close(timeoutch)
		return nil, fmt.Errorf("connect error: %s\n", c.GoString(libuv.Strerror(libuv.Errno(status))))
	}
	pconn = &persistConn{
		conn:      conn,
		t:         t,
		reqch:     make(chan requestAndChan, 1),
		cancelch:  make(chan freeChan, 1),
		timeoutch: timeoutch,
		//writech: make(chan writeRequest, 1),
		//closech: make(chan struct{}),
	}

	net.Freeaddrinfo(res)

	if pconn.conn.IsCompleted != 1 {
		go pconn.readWriteLoop(loop)
	}
	return pconn, nil
}

func (pc *persistConn) roundTrip(req *Request) (*Response, error) {
	resc := make(chan responseAndError, 1)

	pc.reqch <- requestAndChan{
		req: req,
		ch:  resc,
	}
	// Determine whether timeout has occurred
	if pc.conn.IsCompleted == 1 {
		rc := <-pc.reqch // blocking
		// Free the resources
		FreeResources(nil, nil, nil, nil, pc, rc)
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
		close(freech)
		return nil, fmt.Errorf("request timeout\n")
	}
}

// readWriteLoop handles the main I/O loop for a persistent connection.
// It processes incoming requests, sends them to the server, and handles responses.
func (pc *persistConn) readWriteLoop(loop *libuv.Loop) {
	// Hookup the IO
	hyperIo := NewIoWithConnReadWrite(pc.conn)

	// We need an executor generally to poll futures
	exec := hyper.NewExecutor()
	// Prepare client options
	opts := hyper.NewClientConnOptions()
	opts.Exec(exec)

	handshakeTask := hyper.Handshake(hyperIo, opts)
	SetTaskId(handshakeTask, Send)

	// Let's wait for the handshake to finish...
	exec.Push(handshakeTask)

	// The polling state machine!
	//for {
	// Poll all ready tasks and act on them...
	rc := <-pc.reqch // blocking
	alive := true
	resp := &Response{
		Request: rc.req,
		Header:  make(Header),
		Trailer: make(Header),
	}
	var bodyWriter *io.PipeWriter
	var respBody *hyper.Body = nil
	for alive {
		select {
		case fc := <-pc.cancelch:
			// Free the resources
			FreeResources(nil, respBody, bodyWriter, exec, pc, rc)
			alive = false
			fc.freech <- struct{}{}
			return
		default:
			task := exec.Poll()
			if task == nil {
				//break
				loop.Run(libuv.RUN_ONCE)
				continue
			}
			switch (TaskId)(uintptr(task.Userdata())) {
			case Send:
				err := CheckTaskType(task, Send)
				if err != nil {
					rc.ch <- responseAndError{err: err}
					// Free the resources
					FreeResources(task, respBody, bodyWriter, exec, pc, rc)
					return
				}

				client := (*hyper.ClientConn)(task.Value())
				task.Free()

				// Send it!
				sendTask := client.Send(rc.req.Req)
				SetTaskId(sendTask, ReceiveResp)
				sendRes := exec.Push(sendTask)
				if sendRes != hyper.OK {
					rc.ch <- responseAndError{err: fmt.Errorf("failed to send request")}
					// Free the resources
					FreeResources(task, respBody, bodyWriter, exec, pc, rc)
					return
				}

				// For this example, no longer need the client
				client.Free()
			case ReceiveResp:
				err := CheckTaskType(task, ReceiveResp)
				if err != nil {
					rc.ch <- responseAndError{err: err}
					// Free the resources
					FreeResources(task, respBody, bodyWriter, exec, pc, rc)
					return
				}

				// Take the results
				hyperResp := (*hyper.Response)(task.Value())
				task.Free()

				readResponseLineAndHeader(resp, hyperResp)
				//err = readTransfer(resp, hyperResp)
				//if err != nil {
				//	rc.ch <- responseAndError{err: err}
				//	// Free the resources
				//	FreeResources(task, respBody, bodyWriter, exec, pc, rc)
				//	return
				//}

				respBody = hyperResp.Body()
				resp.Body, bodyWriter = io.Pipe()

				rc.ch <- responseAndError{res: resp}

				// Response has been returned, stop the timer
				pc.conn.IsCompleted = 1
				// Stop the timer
				if rc.req.timeout != 0 {
					pc.conn.TimeoutTimer.Stop()
					(*libuv.Handle)(c.Pointer(&pc.conn.TimeoutTimer)).Close(nil)
				}

				dataTask := respBody.Data()
				SetTaskId(dataTask, ReceiveRespBody)
				exec.Push(dataTask)

				// No longer need the response
				hyperResp.Free()
			case ReceiveRespBody:
				err := CheckTaskType(task, ReceiveRespBody)
				if err != nil {
					rc.ch <- responseAndError{err: err}
					// Free the resources
					FreeResources(task, respBody, bodyWriter, exec, pc, rc)
					return
				}

				if task.Type() == hyper.TaskBuf {
					buf := (*hyper.Buf)(task.Value())
					bufLen := buf.Len()
					bytes := unsafe.Slice((*byte)(buf.Bytes()), bufLen)
					if bodyWriter == nil {
						rc.ch <- responseAndError{err: fmt.Errorf("ResponseBodyWriter is nil")}
						// Free the resources
						FreeResources(task, respBody, bodyWriter, exec, pc, rc)
						return
					}
					_, err := bodyWriter.Write(bytes) // blocking
					if err != nil {
						rc.ch <- responseAndError{err: err}
						// Free the resources
						FreeResources(task, respBody, bodyWriter, exec, pc, rc)
						return
					}
					buf.Free()
					task.Free()

					dataTask := respBody.Data()
					SetTaskId(dataTask, ReceiveRespBody)
					exec.Push(dataTask)

					break
				}

				// We are done with the response body
				if task.Type() != hyper.TaskEmpty {
					c.Printf(c.Str("unexpected task type\n"))
					rc.ch <- responseAndError{err: fmt.Errorf("unexpected task type\n")}
					// Free the resources
					FreeResources(task, respBody, bodyWriter, exec, pc, rc)
					return
				}

				// Free the resources
				FreeResources(task, respBody, bodyWriter, exec, pc, rc)

				alive = false
			case NotSet:
				// A background task for hyper_client completed...
				task.Free()
			}
		}
	}
	//}
}

// OnConnect is the libuv callback for a successful connection
func OnConnect(req *libuv.Connect, status c.Int) {
	//conn := (*ConnData)(req.Data)
	//conn := (*struct{ data *ConnData })(c.Pointer(req)).data
	conn := (*ConnData)((*libuv.Req)(c.Pointer(req)).GetData())

	if status < 0 {
		c.Fprintf(c.Stderr, c.Str("connect error: %d\n"), libuv.Strerror(libuv.Errno(status)))
		return
	}
	(*libuv.Stream)(c.Pointer(&conn.TcpHandle)).StartRead(AllocBuffer, OnRead)
}

// AllocBuffer allocates a buffer for reading from a socket
func AllocBuffer(handle *libuv.Handle, suggestedSize uintptr, buf *libuv.Buf) {
	//conn := (*ConnData)(handle.Data)
	//conn := (*struct{ data *ConnData })(c.Pointer(handle)).data
	conn := (*ConnData)(handle.GetData())
	if conn.ReadBuf.Base == nil {
		conn.ReadBuf = libuv.InitBuf((*c.Char)(c.Malloc(suggestedSize)), c.Uint(suggestedSize))
		conn.ReadBufFilled = 0
	}
	*buf = libuv.InitBuf((*c.Char)(c.Pointer(uintptr(c.Pointer(conn.ReadBuf.Base))+conn.ReadBufFilled)), c.Uint(suggestedSize-conn.ReadBufFilled))
}

// OnRead is the libuv callback for reading from a socket
// This callback function is called when data is available to be read
func OnRead(stream *libuv.Stream, nread c.Long, buf *libuv.Buf) {
	// Get the connection data associated with the stream
	conn := (*ConnData)((*libuv.Handle)(c.Pointer(stream)).GetData())
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

// ReadCallBack read callback function for Hyper library
func ReadCallBack(userdata c.Pointer, ctx *hyper.Context, buf *uint8, bufLen uintptr) uintptr {
	// Get the user data (connection data)
	conn := (*ConnData)(userdata)

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

// OnWrite is the libuv callback for writing to a socket
// Callback function called after a write operation completes
func OnWrite(req *libuv.Write, status c.Int) {
	// Get the connection data associated with the write request
	conn := (*ConnData)((*libuv.Req)(c.Pointer(req)).GetData())
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

// WriteCallBack write callback function for Hyper library
func WriteCallBack(userdata c.Pointer, ctx *hyper.Context, buf *uint8, bufLen uintptr) uintptr {
	// Get the user data (connection data)
	conn := (*ConnData)(userdata)
	// Create a libuv buffer
	initBuf := libuv.InitBuf((*c.Char)(c.Pointer(buf)), c.Uint(bufLen))
	//req := (*libuv.Write)(c.Malloc(unsafe.Sizeof(libuv.Write{})))
	req := &libuv.Write{}
	// Associate the connection data with the write request
	(*libuv.Req)(c.Pointer(req)).SetData(c.Pointer(conn))
	//req.Data = c.Pointer(conn)

	// Perform the asynchronous write operation
	ret := req.Write((*libuv.Stream)(c.Pointer(&conn.TcpHandle)), &initBuf, 1, OnWrite)
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

// OnTimeout is the libuv callback for a timeout
func OnTimeout(handle *libuv.Timer) {
	ct := (*connAndTimeoutChan)((*libuv.Handle)(c.Pointer(handle)).GetData())
	if ct.conn.IsCompleted != 1 {
		ct.conn.IsCompleted = 1
		ct.timeoutch <- struct{}{}
	}
	// Close the timer
	(*libuv.Handle)(c.Pointer(&ct.conn.TimeoutTimer)).Close(nil)
}

// NewIoWithConnReadWrite creates a new IO with read and write callbacks
func NewIoWithConnReadWrite(connData *ConnData) *hyper.Io {
	hyperIo := hyper.NewIo()
	hyperIo.SetUserdata(c.Pointer(connData))
	hyperIo.SetRead(ReadCallBack)
	hyperIo.SetWrite(WriteCallBack)
	return hyperIo
}

// SetTaskId Set TaskId to the task's userdata as a unique identifier
func SetTaskId(task *hyper.Task, userData TaskId) {
	var data = userData
	task.SetUserdata(unsafe.Pointer(uintptr(data)))
}

// CheckTaskType checks the task type
func CheckTaskType(task *hyper.Task, curTaskId TaskId) error {
	switch curTaskId {
	case Send:
		if task.Type() == hyper.TaskError {
			c.Printf(c.Str("handshake task error!\n"))
			return Fail((*hyper.Error)(task.Value()))
		}
		if task.Type() != hyper.TaskClientConn {
			return fmt.Errorf("unexpected task type\n")
		}
		return nil
	case ReceiveResp:
		if task.Type() == hyper.TaskError {
			c.Printf(c.Str("send task error!\n"))
			return Fail((*hyper.Error)(task.Value()))
		}
		if task.Type() != hyper.TaskResponse {
			c.Printf(c.Str("unexpected task type\n"))
			return fmt.Errorf("unexpected task type\n")
		}
		return nil
	case ReceiveRespBody:
		if task.Type() == hyper.TaskError {
			c.Printf(c.Str("body error!\n"))
			return Fail((*hyper.Error)(task.Value()))
		}
		return nil
	case NotSet:
	}
	return fmt.Errorf("unexpected TaskId\n")
}

// Fail prints the error details and panics
func Fail(err *hyper.Error) error {
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

// FreeResources frees the resources
func FreeResources(task *hyper.Task, respBody *hyper.Body, bodyWriter *io.PipeWriter, exec *hyper.Executor, pc *persistConn, rc requestAndChan) {
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
	FreeConnData(pc.conn)

	CloseChannels(rc, pc)
}

// CloseChannels closes the channels
func CloseChannels(rc requestAndChan, pc *persistConn) {
	// Closing the channel
	close(rc.ch)
	close(pc.reqch)
	close(pc.timeoutch)
	close(pc.cancelch)
}

// FreeConnData frees the connection data
func FreeConnData(conn *ConnData) {
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
