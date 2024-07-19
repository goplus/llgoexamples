package hyper

import (
	"fmt"
	"strings"
	_ "unsafe"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/net"
	"github.com/goplus/llgo/c/os"
	"github.com/goplus/llgo/c/sys"
	"github.com/goplus/llgo/c/syscall"
)

const (
	LLGoPackage = "link: $(pkg-config --libs hyper); -lhyper"
)

const (
	IterContinue = 0
	IterBreak    = 1
	IoPending    = 4294967295
	IoError      = 4294967294
	PollReady    = 0
	PollPending  = 1
	PollError    = 3
)

type HTTPVersion c.Int

const (
	HTTPVersionNone HTTPVersion = 0
	HTTPVersion10   HTTPVersion = 10
	HTTPVersion11   HTTPVersion = 11
	HTTPVersion2    HTTPVersion = 20
)

type Code c.Int

const (
	OK Code = iota
	ERROR
	InvalidArg
	UnexpectedEOF
	AbortedByCallback
	FeatureNotEnabled
	InvalidPeerMessage
)

type TaskReturnType c.Int

const (
	TaskEmpty TaskReturnType = iota
	TaskError
	TaskClientConn
	TaskResponse
	TaskBuf
)

type ExampleId c.Int

const (
	ExampleNotSet ExampleId = iota
	ExampleHandshake
	ExampleSend
	ExampleRespBody
)

type Body struct {
	Unused [0]byte
}

type Buf struct {
	Unused [0]byte
}

type ClientConn struct {
	Unused [0]byte
}

type ClientConnOptions struct {
	Unused [0]byte
}

type Context struct {
	Unused [0]byte
}

type Error struct {
	Unused [0]byte
}

type Executor struct {
	Unused [0]byte
}

type Headers struct {
	Unused [0]byte
}

type Io struct {
	Unused [0]byte
}

type Request struct {
	Unused [0]byte
}

type Response struct {
	Unused [0]byte
}

type Task struct {
	Unused [0]byte
}

type Waker struct {
	Unused [0]byte
}

// llgo:type C
type BodyForeachCallback func(c.Pointer, *Buf) c.Int

// llgo:type C
type BodyDataCallback func(c.Pointer, *Context, **Buf) c.Int

// llgo:type C
type RequestOnInformationalCallback func(c.Pointer, *Response)

// llgo:type C
type HeadersForeachCallback func(c.Pointer, *uint8, uintptr, *uint8, uintptr) c.Int

// llgo:type C
type IoReadCallback func(c.Pointer, *Context, *uint8, uintptr) uintptr

// llgo:type C
type IoWriteCallback func(c.Pointer, *Context, *uint8, uintptr) uintptr

// Returns a static ASCII (null terminated) string of the hyper_client version.
// llgo:link Version C.hyper_version
func Version() *c.Char { return nil }

// Creates a new "empty" body.
// llgo:link NewBody C.hyper_body_new
func NewBody() *Body {
	return nil
}

// Free a body.
// llgo:link (*Body).Free C.hyper_body_free
func (body *Body) Free() {}

// Creates a task that will poll a response body for the next buffer of data.
// llgo:link (*Body).Data C.hyper_body_data
func (body *Body) Data() *Task {
	return nil
}

// Creates a task to execute the callback with each body chunk received.
// llgo:link (*Body).Foreach C.hyper_body_foreach
func (body *Body) Foreach(callback BodyForeachCallback, userdata c.Pointer) *Task {
	return nil
}

// Set userdata on this body, which will be passed to callback functions.
// llgo:link (*Body).SetUserdata C.hyper_body_set_userdata
func (body *Body) SetUserdata(userdata c.Pointer) {}

// Set the outgoing data callback for this body.
// llgo:link (*Body).SetDataFunc C.hyper_body_set_data_func
func (body *Body) SetDataFunc(callback BodyDataCallback) {}

// Create a new `hyper_buf *` by copying the provided bytes.
// llgo:link CopyBuf C.hyper_buf_copy
func CopyBuf(buf *uint8, len uintptr) *Buf {
	return nil
}

// Get a pointer to the bytes in this buffer.
// llgo:link (*Buf).Bytes C.hyper_buf_bytes
func (buf *Buf) Bytes() *uint8 {
	return nil
}

// Get the length of the bytes this buffer contains.
// llgo:link (*Buf).Len C.hyper_buf_len
func (buf *Buf) Len() uintptr {
	return 0
}

// Free this buffer.
// llgo:link (*Buf).Free C.hyper_buf_free
func (buf *Buf) Free() {}

// Creates an HTTP client handshake task.
// llgo:link Handshake C.hyper_clientconn_handshake
func Handshake(io *Io, options *ClientConnOptions) *Task {
	return nil
}

// Creates a task to send a request on the client connection.
// llgo:link (*ClientConn).Send C.hyper_clientconn_send
func (conn *ClientConn) Send(req *Request) *Task {
	return nil
}

// Free a `hyper_clientconn *`.
// llgo:link (*ClientConn).Free C.hyper_clientconn_free
func (conn *ClientConn) Free() {}

// Creates a new set of HTTP clientconn options to be used in a handshake.
// llgo:link NewClientConnOptions C.hyper_clientconn_options_new
func NewClientConnOptions() *ClientConnOptions {
	return nil
}

// Set whether header case is preserved.
// llgo:link (*ClientConnOptions).SetPreserveHeaderCase C.hyper_clientconn_options_set_preserve_header_case
func (opts *ClientConnOptions) SetPreserveHeaderCase(enabled c.Int) {}

// Set whether header order is preserved.
// llgo:link (*ClientConnOptions).SetPreserveHeaderOrder C.hyper_clientconn_options_set_preserve_header_order
func (opts *ClientConnOptions) SetPreserveHeaderOrder(enabled c.Int) {}

// Free a set of HTTP clientconn options.
// llgo:link (*ClientConnOptions).Free C.hyper_clientconn_options_free
func (opts *ClientConnOptions) Free() {}

// Set the client background task executor.
// llgo:link (*ClientConnOptions).Exec C.hyper_clientconn_options_exec
func (opts *ClientConnOptions) Exec(exec *Executor) {}

// Set whether to use HTTP2.
// llgo:link (*ClientConnOptions).HTTP2 C.hyper_clientconn_options_http2
func (opts *ClientConnOptions) HTTP2(enabled c.Int) Code {
	return 0
}

// Set whether HTTP/1 connections accept obsolete line folding for header values.
// llgo:link (*ClientConnOptions).HTTP1AllowMultilineHeaders C.hyper_clientconn_options_http1_allow_multiline_headers
func (opts *ClientConnOptions) HTTP1AllowMultilineHeaders(enabled c.Int) Code {
	return 0
}

// Frees a `hyper_error`.
// llgo:link (*Error).Free C.hyper_error_free
func (err *Error) Free() {
}

// Get an equivalent `c.Int` from this error.
// llgo:link (*Error).Code C.hyper_error_code
func (err *Error) Code() Code {
	return 0
}

// Print the details of this error to a buffer.
// llgo:link (*Error).Print C.hyper_error_print
func (err *Error) Print(dst *uint8, dstLen uintptr) uintptr {
	return 0
}

// Construct a new HTTP request.
// llgo:link NewRequest C.hyper_request_new
func NewRequest() *Request {
	return nil
}

// Free an HTTP request.
// llgo:link (*Request).Free C.hyper_request_free
func (req *Request) Free() {}

// Set the HTTP Method of the request.
// llgo:link (*Request).SetMethod C.hyper_request_set_method
func (req *Request) SetMethod(method *uint8, methodLen uintptr) Code {
	return 0
}

// Set the URI of the request.
// llgo:link (*Request).SetURI C.hyper_request_set_uri
func (req *Request) SetURI(uri *uint8, uriLen uintptr) Code {
	return 0
}

// Set the URI of the request with separate scheme, authority, and
// llgo:link (*Request).SetURIParts C.hyper_request_set_uri_parts
func (req *Request) SetURIParts(scheme *uint8, schemeLen uintptr, authority *uint8, authorityLen uintptr, pathAndQuery *uint8, pathAndQueryLen uintptr) Code {
	return 0
}

// Set the preferred HTTP version of the request.
// llgo:link (*Request).SetVersion C.hyper_request_set_version
func (req *Request) SetVersion(version c.Int) Code {
	return 0
}

// Gets a mutable reference to the HTTP headers of this request.
// llgo:link (*Request).Headers C.hyper_request_headers
func (req *Request) Headers() *Headers {
	return nil
}

// Set the body of the request.
// llgo:link (*Request).SetBody C.hyper_request_set_body
func (req *Request) SetBody(body *Body) Code {
	return 0
}

// Set an informational (1xx) response callback.
// llgo:link (*Request).OnInformational C.hyper_request_on_informational
func (req *Request) OnInformational(callback RequestOnInformationalCallback, data c.Pointer) Code {
	return 0
}

// Free an HTTP response.
// llgo:link (*Response).Free C.hyper_response_free
func (resp *Response) Free() {}

// Get the HTTP-Status code of this response.
// llgo:link (*Response).Status C.hyper_response_status
func (resp *Response) Status() uint16 {
	return 0
}

// Get a pointer to the reason-phrase of this response.
// llgo:link (*Response).ReasonPhrase C.hyper_response_reason_phrase
func (resp *Response) ReasonPhrase() *uint8 {
	return nil
}

// Get the length of the reason-phrase of this response.
// llgo:link (*Response).ReasonPhraseLen C.hyper_response_reason_phrase_len
func (resp *Response) ReasonPhraseLen() uintptr {
	return 0
}

// Get the HTTP version used by this response.
// llgo:link (*Response).Version C.hyper_response_version
func (resp *Response) Version() c.Int {
	return 0
}

// Gets a reference to the HTTP headers of this response.
// llgo:link (*Response).Headers C.hyper_response_headers
func (resp *Response) Headers() *Headers {
	return nil
}

// Take ownership of the body of this response.
// llgo:link (*Response).Body C.hyper_response_body
func (resp *Response) Body() *Body {
	return nil
}

// Iterates the headers passing each name and value pair to the callback.
// llgo:link (*Headers).Foreach C.hyper_headers_foreach
func (headers *Headers) Foreach(callback HeadersForeachCallback, userdata c.Pointer) {}

// Sets the header with the provided name to the provided value.
// llgo:link (*Headers).Set C.hyper_headers_set
func (headers *Headers) Set(name *uint8, nameLen uintptr, value *uint8, valueLen uintptr) Code {
	return 0
}

// Adds the provided value to the list of the provided name.
// llgo:link (*Headers).Add C.hyper_headers_add
func (headers *Headers) Add(name *uint8, nameLen uintptr, value *uint8, valueLen uintptr) Code {
	return 0
}

// Create a new IO type used to represent a transport.
// llgo:link NewIo C.hyper_io_new
func NewIo() *Io {
	return nil
}

// Free an IO handle.
// llgo:link (*Io).Free C.hyper_io_free
func (io *Io) Free() {}

// Set the user data pointer for this IO to some value.
// llgo:link (*Io).SetUserdata C.hyper_io_set_userdata
func (io *Io) SetUserdata(data c.Pointer) {}

// Set the read function for this IO transport.
// llgo:link (*Io).SetRead C.hyper_io_set_read
func (io *Io) SetRead(callback IoReadCallback) {}

// Set the write function for this IO transport.
// llgo:link (*Io).SetWrite C.hyper_io_set_write
func (io *Io) SetWrite(callback IoWriteCallback) {}

// Creates a new task executor.
// llgo:link NewExecutor C.hyper_executor_new
func NewExecutor() *Executor {
	return nil
}

// Frees an executor and any incomplete tasks still part of it.
// llgo:link (*Executor).Free C.hyper_executor_free
func (exec *Executor) Free() {}

// Push a task onto the executor.
// llgo:link (*Executor).Push C.hyper_executor_push
func (exec *Executor) Push(task *Task) Code {
	return 0
}

// Polls the executor, trying to make progress on any tasks that can do so.
// llgo:link (*Executor).Poll C.hyper_executor_poll
func (exec *Executor) Poll() *Task {
	return nil
}

// Free a task.
// llgo:link (*Task).Free C.hyper_task_free
func (task *Task) Free() {}

// Takes the output value of this task.
// llgo:link (*Task).Value C.hyper_task_value
func (task *Task) Value() c.Pointer {
	return nil
}

// Query the return type of this task.
// llgo:link (*Task).Type C.hyper_task_type
func (task *Task) Type() TaskReturnType {
	return 0
}

// Set a user data pointer to be associated with this task.
// llgo:link (*Task).SetUserdata C.hyper_task_set_userdata
func (task *Task) SetUserdata(userdata c.Pointer) {}

// Retrieve the userdata that has been set via `hyper_task_set_userdata`.
// llgo:link (*Task).Userdata C.hyper_task_userdata
func (task *Task) Userdata() c.Pointer {
	return nil
}

// Creates a waker associated with the task context.
// llgo:link (*Context).Waker C.hyper_context_waker
func (cx *Context) Waker() *Waker {
	return nil
}

// Free a waker.
// llgo:link (*Waker).Free C.hyper_waker_free
func (waker *Waker) Free() {}

// Wake up the task associated with a waker.
// llgo:link (*Waker).Wake C.hyper_waker_wake
func (waker *Waker) Wake() {}

type ConnData struct {
	Fd         c.Int
	ReadWaker  *Waker
	WriteWaker *Waker
}

func ConnectTo(host string, port string) c.Int {
	c.Printf(c.Str("connecting to port %s on %s...\n"), c.AllocaCStr(port), c.AllocaCStr(host))
	var hints net.AddrInfo
	hints.Family = net.AF_UNSPEC
	hints.SockType = net.SOCK_STREAM

	var result, rp *net.AddrInfo

	if net.Getaddrinfo(c.AllocaCStr(host), c.AllocaCStr(port), &hints, &result) != 0 {
		panic(fmt.Sprintf("dns failed for %s\n", host))
	}

	var sfd c.Int
	for rp = result; rp != nil; rp = rp.Next {
		sfd = net.Socket(rp.Family, rp.SockType, rp.Protocol)
		if sfd == -1 {
			continue
		}
		if net.Connect(sfd, rp.Addr, rp.AddrLen) != -1 {
			break
		}
		os.Close(sfd)
	}

	net.Freeaddrinfo(result)

	// no address succeeded
	if rp == nil || sfd < 0 {
		panic(fmt.Sprintf("connect failed for %s\n", host))
	}

	c.Printf(c.Str("connected to %s\n"), c.AllocaCStr(host))

	if os.Fcntl(sfd, os.F_SETFL, os.O_NONBLOCK) != 0 {
		panic("failed to set net to non-blocking\n")
	}
	return sfd
}

func ReadCallBack(userdata c.Pointer, ctx *Context, buf *uint8, bufLen uintptr) uintptr {
	conn := (*ConnData)(userdata)

	ret := os.Read(conn.Fd, c.Pointer(buf), bufLen)

	if ret >= 0 {
		return uintptr(ret)
	}

	if os.Errno != os.EAGAIN {
		c.Perror(c.Str("[read callback fail]"))
		// kaboom
		return IoError
	}

	// would block, register interest
	if conn.ReadWaker != nil {
		conn.ReadWaker.Free()
	}
	conn.ReadWaker = ctx.Waker()
	return IoPending
}

func WriteCallBack(userdata c.Pointer, ctx *Context, buf *uint8, bufLen uintptr) uintptr {
	conn := (*ConnData)(userdata)
	ret := os.Write(conn.Fd, c.Pointer(buf), bufLen)

	if int(ret) >= 0 {
		return uintptr(ret)
	}

	if os.Errno != os.EAGAIN {
		c.Perror(c.Str("[write callback fail]"))
		// kaboom
		return IoError
	}

	// would block, register interest
	if conn.WriteWaker != nil {
		conn.WriteWaker.Free()
	}
	conn.WriteWaker = ctx.Waker()
	return IoPending
}

func PrintEachHeader(userdata c.Pointer, name *uint8, nameLen uintptr, value *uint8, valueLen uintptr) c.Int {
	//c.Printf(c.Str("%.*s: %.*s\n"), int(nameLen), name, int(valueLen), value)
	return IterContinue
}

func PrintEachChunk(userdata c.Pointer, chunk *Buf) c.Int {
	buf := chunk.Bytes()
	len := chunk.Len()

	os.Write(1, c.Pointer(buf), len)

	return IterContinue
}

func FreeConnData(conn *ConnData) {
	if conn.ReadWaker != nil {
		conn.ReadWaker.Free()
		conn.ReadWaker = nil
	}
	if conn.WriteWaker != nil {
		conn.WriteWaker.Free()
		conn.WriteWaker = nil
	}
}

func NewConnData(fd c.Int) *ConnData {
	return &ConnData{Fd: fd, ReadWaker: nil, WriteWaker: nil}
}

func NewIoWithConnReadWrite(connData *ConnData) *Io {
	io := NewIo()
	io.SetUserdata(c.Pointer(connData))
	io.SetRead(ReadCallBack)
	io.SetWrite(WriteCallBack)
	return io
}

func (task *Task) SetUserData(userData ExampleId) {
	var data = userData
	task.SetUserdata(c.Pointer(uintptr(data)))
}

type RequestConfig struct {
	ReqMethod      string
	ReqHost        string
	ReqPort        string
	ReqUri         string
	ReqHeaders     map[string]string
	ReqHTTPVersion HTTPVersion
	TimeoutSec     int64
	TimeoutUsec    int32
	//ReqBody
	//ReqURIParts
}

type RequestResponse struct {
	Status  uint16
	Message string
	Content []byte
}

func SendRequest(requestConfig *RequestConfig) *RequestResponse {
	// Parsing request configuration
	method := strings.ToUpper(requestConfig.ReqMethod)
	if method == "" {
		method = "GET"
	}
	if method != "GET" && method != "POST" {
		panic(fmt.Sprintf("There is no method called %s\n", method))
	}
	host := requestConfig.ReqHost
	port := requestConfig.ReqPort
	path := requestConfig.ReqUri
	headers := requestConfig.ReqHeaders

	req := NewRequest()

	// Prepare the request
	// Set the request method and uri
	if req.SetMethod((*uint8)(&[]byte(method)[0]), c.Strlen(c.AllocaCStr(method))) != OK {
		panic(fmt.Sprintf("error setting method %s\n", method))
	}
	if req.SetURI((*uint8)(&[]byte(path)[0]), c.Strlen(c.AllocaCStr(path))) != OK {
		panic(fmt.Sprintf("error setting uri %s\n", path))
	}

	// Set the request headers
	reqHeaders := req.Headers()
	if reqHeaders.Set((*uint8)(&[]byte("Host")[0]), c.Strlen(c.Str("Host")), (*uint8)(&[]byte(host)[0]), c.Strlen(c.AllocaCStr(host))) != OK {
		panic("error setting headers\n")
	}

	if headers != nil && len(headers) > 0 {
		for k, v := range headers {
			if reqHeaders.Set((*uint8)(&[]byte(k)[0]), c.Strlen(c.AllocaCStr(k)), (*uint8)(&[]byte(v)[0]), c.Strlen(c.AllocaCStr(v))) != OK {
				panic("error setting headers\n")
			}
		}
	}

	// Set the request http version
	if requestConfig.ReqHTTPVersion != 0 {
		req.SetVersion(c.Int(requestConfig.ReqHTTPVersion))
	}

	// TODO set the uri parts
	// TODO set the request body

	//var response RequestResponse

	fd := ConnectTo(host, port)

	connData := NewConnData(fd)

	// Hookup the IO
	io := NewIoWithConnReadWrite(connData)

	// We need an executor generally to poll futures
	exec := NewExecutor()

	// Prepare client options
	opts := NewClientConnOptions()
	opts.Exec(exec)

	handshakeTask := Handshake(io, opts)
	handshakeTask.SetUserData(ExampleHandshake)

	// Let's wait for the handshake to finish...
	exec.Push(handshakeTask)

	var fdsRead, fdsWrite, fdsExcep syscall.FdSet
	var err *Error
	var response RequestResponse

	// The polling state machine!
	for {
		// Poll all ready tasks and act on them...
		for {
			task := exec.Poll()

			if task == nil {
				break
			}

			switch (ExampleId)(uintptr(task.Userdata())) {
			case ExampleHandshake:
				if task.Type() == TaskError {
					c.Printf(c.Str("handshake error!\n"))
					err = (*Error)(task.Value())
					fail(err)
				}
				if task.Type() != TaskClientConn {
					c.Printf(c.Str("169 unexpected task type\n"))
					fail(err)
				}

				c.Printf(c.Str("preparing http request ...\n"))

				client := (*ClientConn)(task.Value())
				task.Free()

				// Send it!
				sendTask := client.Send(req)
				sendTask.SetUserData(ExampleSend)
				c.Printf(c.Str("sending ...\n"))
				sendRes := exec.Push(sendTask)
				if sendRes != OK {
					panic("error send\n")
				}

				// For this example, no longer need the client
				client.Free()

				break
			case ExampleSend:
				if task.Type() == TaskError {
					c.Printf(c.Str("send error!\n"))
					err = (*Error)(task.Value())
					fail(err)
				}
				if task.Type() != TaskResponse {
					c.Printf(c.Str("unexpected task type\n"))
					fail(err)
				}

				// Take the results
				resp := (*Response)(task.Value())
				task.Free()

				rp := resp.ReasonPhrase()
				rpLen := resp.ReasonPhraseLen()

				response.Status = resp.Status()
				response.Message = string((*[1 << 30]byte)(c.Pointer(rp))[:rpLen:rpLen])

				headers := resp.Headers()
				headers.Foreach(PrintEachHeader, nil)
				c.Printf(c.Str("\n"))

				respBody := resp.Body()
				foreachTask := respBody.Foreach(PrintEachChunk, nil)
				foreachTask.SetUserData(ExampleRespBody)
				exec.Push(foreachTask)

				// No longer need the response
				resp.Free()

				break
			case ExampleRespBody:
				if task.Type() == TaskError {
					c.Printf(c.Str("body error!\n"))
					err = (*Error)(task.Value())
					fail(err)
				}
				if task.Type() != TaskEmpty {
					c.Printf(c.Str("unexpected task type\n"))
					fail(err)
				}

				// Cleaning up before exiting
				task.Free()
				exec.Free()
				FreeConnData(connData)

				return &response
			case ExampleNotSet:
				// A background task for hyper_client completed...
				task.Free()
				break
			}
		}

		// All futures are pending on IO work, so select on the fds.

		sys.FD_ZERO(&fdsRead)
		sys.FD_ZERO(&fdsWrite)
		sys.FD_ZERO(&fdsExcep)

		if connData.ReadWaker != nil {
			sys.FD_SET(connData.Fd, &fdsRead)
		}
		if connData.WriteWaker != nil {
			sys.FD_SET(connData.Fd, &fdsWrite)
		}

		// Set the request timeout
		var tv syscall.Timeval
		if requestConfig.TimeoutSec != 0 || requestConfig.TimeoutUsec != 0 {
			tv.Sec = requestConfig.TimeoutSec
			tv.Usec = requestConfig.TimeoutUsec
		} else {
			tv.Sec = 10
		}

		selRet := sys.Select(connData.Fd+1, &fdsRead, &fdsWrite, &fdsExcep, &tv)
		if selRet < 0 {
			panic("select() error\n")
		} else if selRet == 0 {
			panic("select() timeout\n")
		}

		if sys.FD_ISSET(connData.Fd, &fdsRead) != 0 {
			connData.ReadWaker.Wake()
			connData.ReadWaker = nil
		}

		if sys.FD_ISSET(connData.Fd, &fdsWrite) != 0 {
			connData.WriteWaker.Wake()
			connData.WriteWaker = nil
		}
	}
}

func fail(err *Error) {
	if err != nil {
		c.Printf(c.Str("error code: %d\n"), err.Code())
		// grab the error details
		var errBuf [256]c.Char
		errLen := err.Print((*uint8)(c.Pointer(&errBuf[:][0])), uintptr(len(errBuf)))

		//c.Printf(c.Str("details: %.*s\n"), c.int(errLen), errBuf)
		c.Printf(c.Str("details: "))
		for i := 0; i < int(errLen); i++ {
			c.Printf(c.Str("%c"), errBuf[i])
		}
		c.Printf(c.Str("\n"))

		// clean up the error
		err.Free()
		panic("request failed\n")
	}
	return
}
