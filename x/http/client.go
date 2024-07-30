package http

import (
	"fmt"
	io2 "io"
	"strconv"
	"strings"
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

type Client struct {
	Transport RoundTripper
}

var DefaultClient = &Client{}

type RoundTripper interface {
	RoundTrip(*hyper.Request) (*Response, error)
}

func (c *Client) transport() RoundTripper {
	if c.Transport != nil {
		return c.Transport
	}
	return DefaultTransport
}

func Get2(url string) (*Response, error) {
	return DefaultClient.Get(url)
}

func (c *Client) Get(url string) (*Response, error) {
	req, err := NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}

func (c *Client) Do(req *hyper.Request) (*Response, error) {
	return c.do(req)
}

func (c *Client) do(req *hyper.Request) (*Response, error) {
	return c.send(req, nil)
}

func (c *Client) send(req *hyper.Request, deadline any) (*Response, error) {
	return send(req, c.transport(), deadline)
}

func send(req *hyper.Request, rt RoundTripper, deadline any) (resp *Response, err error) {
	return rt.RoundTrip(req)
}

func NewRequest(method, url string, body io2.Reader) (*hyper.Request, error) {
	host, _, uri := parseURL(url)
	// Prepare the request
	req := hyper.NewRequest()
	// Set the request method and uri
	if req.SetMethod((*uint8)(&[]byte(method)[0]), c.Strlen(c.AllocaCStr(method))) != hyper.OK {
		return nil, fmt.Errorf("error setting method %s\n", method)
	}
	if req.SetURI((*uint8)(&[]byte(uri)[0]), c.Strlen(c.AllocaCStr(uri))) != hyper.OK {
		return nil, fmt.Errorf("error setting uri %s\n", uri)
	}

	// Set the request headers
	reqHeaders := req.Headers()
	if reqHeaders.Set((*uint8)(&[]byte("Host")[0]), c.Strlen(c.Str("Host")), (*uint8)(&[]byte(host)[0]), c.Strlen(c.AllocaCStr(host))) != hyper.OK {
		return nil, fmt.Errorf("error setting headers\n")
	}
	return req, nil
}

func Get(url string) (_ *Response, err error) {
	host, port, uri := parseURL(url)

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

	net.Freeaddrinfo(res)

	// Hookup the IO
	io := NewIoWithConnReadWrite(conn)

	// We need an executor generally to poll futures
	exec := hyper.NewExecutor()

	// Prepare client options
	opts := hyper.NewClientConnOptions()
	opts.Exec(exec)

	handshakeTask := hyper.Handshake(io, opts)
	SetUserData(handshakeTask, hyper.ExampleHandshake)

	// Let's wait for the handshake to finish...
	exec.Push(handshakeTask)

	var hyperErr *hyper.Error
	var response Response

	// The polling state machine!
	for {
		// Poll all ready tasks and act on them...
		for {
			task := exec.Poll()
			if task == nil {
				break
			}

			switch (hyper.ExampleId)(uintptr(task.Userdata())) {
			case hyper.ExampleHandshake:
				if task.Type() == hyper.TaskError {
					c.Printf(c.Str("handshake error!\n"))
					hyperErr = (*hyper.Error)(task.Value())
					err = Fail(hyperErr)
					return nil, err
				}
				if task.Type() != hyper.TaskClientConn {
					c.Printf(c.Str("unexpected task type\n"))
					err = Fail(hyperErr)
					return nil, err
				}

				client := (*hyper.ClientConn)(task.Value())
				task.Free()

				// Prepare the request
				req := hyper.NewRequest()
				// Set the request method and uri
				if req.SetMethod((*uint8)(&[]byte("GET")[0]), c.Strlen(c.Str("GET"))) != hyper.OK {
					return nil, fmt.Errorf("error setting method %s\n", "GET")
				}
				if req.SetURI((*uint8)(&[]byte(uri)[0]), c.Strlen(c.AllocaCStr(uri))) != hyper.OK {
					return nil, fmt.Errorf("error setting uri %s\n", uri)
				}

				// Set the request headers
				reqHeaders := req.Headers()
				if reqHeaders.Set((*uint8)(&[]byte("Host")[0]), c.Strlen(c.Str("Host")), (*uint8)(&[]byte(host)[0]), c.Strlen(c.AllocaCStr(host))) != hyper.OK {
					return nil, fmt.Errorf("error setting headers\n")
				}

				// Send it!
				sendTask := client.Send(req)
				SetUserData(sendTask, hyper.ExampleSend)
				sendRes := exec.Push(sendTask)
				if sendRes != hyper.OK {
					return nil, fmt.Errorf("error send\n")
				}

				// For this example, no longer need the client
				client.Free()

				break
			case hyper.ExampleSend:
				if task.Type() == hyper.TaskError {
					c.Printf(c.Str("send error!\n"))
					hyperErr = (*hyper.Error)(task.Value())
					err = Fail(hyperErr)
					return nil, err
				}
				if task.Type() != hyper.TaskResponse {
					c.Printf(c.Str("unexpected task type\n"))
					err = Fail(hyperErr)
					return nil, err
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
				respBody := resp.Body()

				response.Body, response.respBodyWriter = io2.Pipe()

				/*go func() {
					fmt.Println("writing...")
					for {
						fmt.Println("writing for...")
						dataTask := respBody.Data()
						exec.Push(dataTask)
						dataTask = exec.Poll()
						if dataTask.Type() == hyper.TaskBuf {
							buf := (*hyper.Buf)(dataTask.Value())
							len := buf.Len()
							bytes := unsafe.Slice((*byte)(buf.Bytes()), len)
							_, err := response.respBodyWriter.Write(bytes)
							if err != nil {
								fmt.Printf("Failed to write response body: %v\n", err)
								break
							}
							dataTask.Free()
						} else if dataTask.Type() == hyper.TaskEmpty {
							fmt.Println("writing empty")
							dataTask.Free()
							break
						}
					}
					fmt.Println("end writing")
					defer response.respBodyWriter.Close()
				}()*/

				foreachTask := respBody.Foreach(AppendToResponseBody, c.Pointer(&response))

				SetUserData(foreachTask, hyper.ExampleRespBody)
				exec.Push(foreachTask)

				return &response, nil

				// No longer need the response
				//resp.Free()

				break
			case hyper.ExampleRespBody:
				println("ExampleRespBody")
				if task.Type() == hyper.TaskError {
					c.Printf(c.Str("body error!\n"))
					hyperErr = (*hyper.Error)(task.Value())
					err = Fail(hyperErr)
					return nil, err
				}
				if task.Type() != hyper.TaskEmpty {
					c.Printf(c.Str("unexpected task type\n"))
					err = Fail(hyperErr)
					return nil, err
				}

				// Cleaning up before exiting
				task.Free()
				exec.Free()
				(*libuv.Handle)(c.Pointer(&conn.TcpHandle)).Close(nil)

				FreeConnData(conn)

				//if response.respBodyWriter != nil {
				//	defer response.respBodyWriter.Close()
				//}

				return &response, nil
			case hyper.ExampleNotSet:
				println("ExampleNotSet")
				// A background task for hyper_client completed...
				task.Free()
				break
			}
		}

		libuv.Run(loop, libuv.RUN_ONCE)
	}
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
func Fail(err *hyper.Error) error {
	if err != nil {
		c.Printf(c.Str("error code: %d\n"), err.Code())
		// grab the error details
		var errBuf [256]c.Char
		errLen := err.Print((*uint8)(c.Pointer(&errBuf[:][0])), uintptr(len(errBuf)))

		c.Printf(c.Str("details: %.*s\n"), c.Int(errLen), c.Pointer(&errBuf[:][0]))

		// clean up the error
		err.Free()
		return fmt.Errorf("hyper error\n")
	}
	return nil
}

// NewIoWithConnReadWrite creates a new IO with read and write callbacks
func NewIoWithConnReadWrite(connData *ConnData) *hyper.Io {
	io := hyper.NewIo()
	io.SetUserdata(c.Pointer(connData))
	io.SetRead(ReadCallBack)
	io.SetWrite(WriteCallBack)
	return io
}

// SetUserData Set the user data for the task
func SetUserData(task *hyper.Task, userData hyper.ExampleId) {
	var data = userData
	task.SetUserdata(c.Pointer(uintptr(data)))
}

// parseURL Parse the URL and extract the host name, port number, and URI
func parseURL(rawURL string) (hostname, port, uri string) {
	// 找到 "://" 的位置，以分隔协议和主机名
	schemeEnd := strings.Index(rawURL, "://")
	if schemeEnd != -1 {
		//scheme = rawURL[:schemeEnd]
		rawURL = rawURL[schemeEnd+3:]
	} else {
		//scheme = "http" // 默认协议为 http
	}

	// 找到第一个 "/" 的位置，以分隔主机名和路径
	pathStart := strings.Index(rawURL, "/")
	if pathStart != -1 {
		uri = rawURL[pathStart:]
		rawURL = rawURL[:pathStart]
	} else {
		uri = "/"
	}

	// 找到 ":" 的位置，以分隔主机名和端口号
	portStart := strings.LastIndex(rawURL, ":")
	if portStart != -1 {
		hostname = rawURL[:portStart]
		port = rawURL[portStart+1:]
	} else {
		hostname = rawURL
		port = "" // 未指定端口号
	}

	// 如果未指定端口号，根据协议设置默认端口号
	if port == "" {
		//if scheme == "https" {
		//	port = "443"
		//} else {
		//	port = "80"
		//}
		port = "80"
	}
	return
}
