package http

import (
	"fmt"
	io2 "io"
	"strconv"
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
	conn  *ConnData
	t     *Transport
	reqch chan requestAndChan // written by roundTrip; read by readLoop
}

// incomparable is a zero-width, non-comparable type. Adding it to a struct
// makes that struct also non-comparable, and generally doesn't add
// any size (as long as it's first).
type incomparable [0]func()

type requestAndChan struct {
	_   incomparable
	req *hyper.Request
	ch  chan responseAndError // unbuffered; always send in select on callerGone
}

// responseAndError is how the goroutine reading from an HTTP/1 server
// communicates with the goroutine doing the RoundTrip.
type responseAndError struct {
	_   incomparable
	res *Response // else use this response (see res method)
	err error
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
	host := req.URL.Hostname()
	port := req.URL.Port()
	if port == "" {
		// Hyper only supports http
		port = "80"
	}
	loop := libuv.DefaultLoop()
	conn := (*ConnData)(c.Malloc(unsafe.Sizeof(ConnData{})))
	if conn == nil {
		return nil, fmt.Errorf("Failed to allocate memory for conn_data\n")
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
		return nil, fmt.Errorf("getaddrinfo error\n")
	}

	//conn.ConnectReq.Data = c.Pointer(conn)
	(*libuv.Req)(c.Pointer(&conn.ConnectReq)).SetData(c.Pointer(conn))
	status = libuv.TcpConnect(&conn.ConnectReq, &conn.TcpHandle, res.Addr, OnConnect)
	if status != 0 {
		return nil, fmt.Errorf("connect error: %s\n", c.GoString(libuv.Strerror(libuv.Errno(status))))
	}
	pconn = &persistConn{
		conn:  conn,
		t:     t,
		reqch: make(chan requestAndChan, 1),
		//writech: make(chan writeRequest, 1),
		//closech: make(chan struct{}),
	}

	net.Freeaddrinfo(res)

	go pconn.readWriteLoop(loop)
	return pconn, nil
}

func (pc *persistConn) roundTrip(req *Request) (resp *Response, err error) {
	resc := make(chan responseAndError)
	pc.reqch <- requestAndChan{
		req: req.Req,
		ch:  resc,
	}

	select {
	case re := <-resc:
		if (re.res == nil) == (re.err == nil) {
			panic(fmt.Sprintf("internal error: exactly one of res or err should be set; nil=%v", re.res == nil))
		}
		if re.err != nil {
			return nil, err
		}
		return re.res, nil
	}
}

func (pc *persistConn) readWriteLoop(loop *libuv.Loop) {
	// Hookup the IO
	io := NewIoWithConnReadWrite(pc.conn)

	// We need an executor generally to poll futures
	exec := hyper.NewExecutor()

	// Prepare client options
	opts := hyper.NewClientConnOptions()
	opts.Exec(exec)

	handshakeTask := hyper.Handshake(io, opts)
	SetTaskId(handshakeTask, Send)

	// Let's wait for the handshake to finish...
	exec.Push(handshakeTask)

	// The polling state machine!
	//for {
	// Poll all ready tasks and act on them...
	rc := <-pc.reqch // blocking
	alive := true
	var hyperErr *hyper.Error
	var response Response
	var respBody *hyper.Body = nil
	for alive {
		task := exec.Poll()
		if task == nil {
			//break
			libuv.Run(loop, libuv.RUN_ONCE)
			continue
		}

		switch (TaskId)(uintptr(task.Userdata())) {
		case Send:
			if task.Type() == hyper.TaskError {
				c.Printf(c.Str("handshake error!\n"))
				hyperErr = (*hyper.Error)(task.Value())
				Fail(hyperErr)
			}
			if task.Type() != hyper.TaskClientConn {
				c.Printf(c.Str("unexpected task type\n"))
				Fail(hyperErr)
			}

			client := (*hyper.ClientConn)(task.Value())
			task.Free()

			// Send it!
			sendTask := client.Send(rc.req)
			SetTaskId(sendTask, ReceiveResp)
			sendRes := exec.Push(sendTask)
			if sendRes != hyper.OK {
				panic("error send\n")
			}

			// For this example, no longer need the client
			client.Free()

		case ReceiveResp:
			if task.Type() == hyper.TaskError {
				c.Printf(c.Str("send error!\n"))
				hyperErr = (*hyper.Error)(task.Value())
				Fail(hyperErr)
			}
			if task.Type() != hyper.TaskResponse {
				c.Printf(c.Str("unexpected task type\n"))
				Fail(hyperErr)
			}

			// Take the results
			resp := (*hyper.Response)(task.Value())
			task.Free()

			rp := resp.ReasonPhrase()
			rpLen := resp.ReasonPhraseLen()

			response.Status = strconv.Itoa(int(resp.Status())) + " " + string((*[1 << 30]byte)(c.Pointer(rp))[:rpLen:rpLen])
			response.StatusCode = int(resp.Status())

			headers := resp.Headers()
			headers.Foreach(AppendToResponseHeader, c.Pointer(&response))
			//respBody := resp.Body()
			respBody = resp.Body()

			response.Body, response.respBodyWriter = io2.Pipe()

			//foreachTask := respBody.Foreach(AppendToResponseBody, c.Pointer(&response))
			//SetTaskId(foreachTask, ReceiveRespBody)
			//exec.Push(foreachTask)

			rc.ch <- responseAndError{res: &response}

			dataTask := respBody.Data()
			SetTaskId(dataTask, ReceiveRespBody)
			exec.Push(dataTask)

			// No longer need the response
			resp.Free()

		case ReceiveRespBody:
			if task.Type() == hyper.TaskError {
				c.Printf(c.Str("body error!\n"))
				hyperErr = (*hyper.Error)(task.Value())
				Fail(hyperErr)
			}
			if task.Type() == hyper.TaskBuf {
				buf := (*hyper.Buf)(task.Value())
				bufLen := buf.Len()
				bytes := unsafe.Slice((*byte)(buf.Bytes()), bufLen)
				_, err := response.respBodyWriter.Write(bytes) // blocking
				if err != nil {
					panic("[readWriteLoop(): case ReceiveRespBody] error write\n")
				}
				buf.Free()
				task.Free()

				dataTask := respBody.Data()
				SetTaskId(dataTask, ReceiveRespBody)
				exec.Push(dataTask)

				break
			}
			// task.Type() == hyper.TaskEmpty
			if task.Type() != hyper.TaskEmpty {
				c.Printf(c.Str("unexpected task type\n"))
				Fail(hyperErr)
			}
			// Cleaning up before exiting
			task.Free()
			respBody.Free()
			response.respBodyWriter.Close()
			exec.Free()
			(*libuv.Handle)(c.Pointer(&pc.conn.TcpHandle)).Close(nil)
			FreeConnData(pc.conn)

			close(rc.ch)
			close(pc.reqch)

			alive = false
		case NotSet:
			// A background task for hyper_client completed...
			task.Free()
		}
	}
	//}
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
func OnRead(stream *libuv.Stream, nread c.Long, buf *libuv.Buf) {
	//conn := (*ConnData)(stream.Data)
	//conn := (*struct{ data *ConnData })(c.Pointer(stream)).data
	conn := (*ConnData)((*libuv.Handle)(c.Pointer(stream)).GetData())
	if nread > 0 {
		conn.ReadBufFilled += uintptr(nread)
	}
	if conn.ReadWaker != nil {
		conn.ReadWaker.Wake()
		conn.ReadWaker = nil
	}
}

// ReadCallBack is the hyper callback for reading from a socket
func ReadCallBack(userdata c.Pointer, ctx *hyper.Context, buf *uint8, bufLen uintptr) uintptr {
	conn := (*ConnData)(userdata)

	if conn.ReadBufFilled > 0 {
		var toCopy uintptr
		if bufLen < conn.ReadBufFilled {
			toCopy = bufLen
		} else {
			toCopy = conn.ReadBufFilled
		}
		c.Memcpy(c.Pointer(buf), c.Pointer(conn.ReadBuf.Base), toCopy)
		c.Memmove(c.Pointer(conn.ReadBuf.Base), c.Pointer(uintptr(c.Pointer(conn.ReadBuf.Base))+toCopy), conn.ReadBufFilled-toCopy)
		conn.ReadBufFilled -= toCopy
		return toCopy
	}

	if conn.ReadWaker != nil {
		conn.ReadWaker.Free()
	}
	conn.ReadWaker = ctx.Waker()
	return hyper.IoPending
}

// OnWrite is the libuv callback for writing to a socket
func OnWrite(req *libuv.Write, status c.Int) {
	//conn := (*ConnData)(req.Data)
	//conn := (*struct{ data *ConnData })(c.Pointer(req)).data
	conn := (*ConnData)((*libuv.Req)(c.Pointer(req)).GetData())

	if conn.WriteWaker != nil {
		conn.WriteWaker.Wake()
		conn.WriteWaker = nil
	}
	c.Free(c.Pointer(req))
}

// WriteCallBack is the hyper callback for writing to a socket
func WriteCallBack(userdata c.Pointer, ctx *hyper.Context, buf *uint8, bufLen uintptr) uintptr {
	conn := (*ConnData)(userdata)
	initBuf := libuv.InitBuf((*c.Char)(c.Pointer(buf)), c.Uint(bufLen))
	req := (*libuv.Write)(c.Malloc(unsafe.Sizeof(libuv.Write{})))
	//req.Data = c.Pointer(conn)
	(*libuv.Req)(c.Pointer(req)).SetData(c.Pointer(conn))
	ret := req.Write((*libuv.Stream)(c.Pointer(&conn.TcpHandle)), &initBuf, 1, OnWrite)

	if ret >= 0 {
		return bufLen
	}

	if conn.WriteWaker != nil {
		conn.WriteWaker.Free()
	}
	conn.WriteWaker = ctx.Waker()
	return hyper.IoPending
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
	c.Free(c.Pointer(conn))
}

// Fail prints the error details and panics
func Fail(err *hyper.Error) {
	if err != nil {
		c.Printf(c.Str("error code: %d\n"), err.Code())
		// grab the error details
		var errBuf [256]c.Char
		errLen := err.Print((*uint8)(c.Pointer(&errBuf[:][0])), uintptr(len(errBuf)))

		c.Printf(c.Str("details: %.*s\n"), c.Int(errLen), c.Pointer(&errBuf[:][0]))

		// clean up the error
		err.Free()
		panic("hyper error \n")
	}
}

// NewIoWithConnReadWrite creates a new IO with read and write callbacks
func NewIoWithConnReadWrite(connData *ConnData) *hyper.Io {
	io := hyper.NewIo()
	io.SetUserdata(c.Pointer(connData))
	io.SetRead(ReadCallBack)
	io.SetWrite(WriteCallBack)
	return io
}

// SetTaskId Set TaskId to the task's userdata as a unique identifier
func SetTaskId(task *hyper.Task, userData TaskId) {
	var data = userData
	task.SetUserdata(c.Pointer(uintptr(data)))
}
