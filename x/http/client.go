package http

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/net"
	"github.com/goplus/llgo/c/os"
	"github.com/goplus/llgo/c/sys"
	"github.com/goplus/llgo/c/syscall"
	"github.com/goplus/llgoexamples/rust/hyper"
)

type ConnData struct {
	Fd         c.Int
	ReadWaker  *hyper.Waker
	WriteWaker *hyper.Waker
}

type RequestConfig struct {
	ReqMethod      string
	ReqHost        string
	ReqPort        string
	ReqUri         string
	ReqHeaders     map[string]string
	ReqHTTPVersion hyper.HTTPVersion
	TimeoutSec     int64
	TimeoutUsec    int32
	//ReqBody
	//ReqURIParts
}

func Get(url string) *Response {
	host, port, uri := parseURL(url)
	req := hyper.NewRequest()

	// Prepare the request
	// Set the request method and uri
	if req.SetMethod((*uint8)(&[]byte("GET")[0]), c.Strlen(c.Str("GET"))) != hyper.OK {
		panic(fmt.Sprintf("error setting method %s\n", "GET"))
	}
	if req.SetURI((*uint8)(&[]byte(uri)[0]), c.Strlen(c.AllocaCStr(uri))) != hyper.OK {
		panic(fmt.Sprintf("error setting uri %s\n", uri))
	}

	// Set the request headers
	reqHeaders := req.Headers()
	if reqHeaders.Set((*uint8)(&[]byte("Host")[0]), c.Strlen(c.Str("Host")), (*uint8)(&[]byte(host)[0]), c.Strlen(c.AllocaCStr(host))) != hyper.OK {
		panic("error setting headers\n")
	}

	//var response RequestResponse

	fd := ConnectTo(host, port)

	connData := NewConnData(fd)

	// Hookup the IO
	io := NewIoWithConnReadWrite(connData)

	// We need an executor generally to poll futures
	exec := hyper.NewExecutor()

	// Prepare client options
	opts := hyper.NewClientConnOptions()
	opts.Exec(exec)

	handshakeTask := hyper.Handshake(io, opts)
	SetUserData(handshakeTask, hyper.ExampleHandshake)

	// Let's wait for the handshake to finish...
	exec.Push(handshakeTask)

	var fdsRead, fdsWrite, fdsExcep syscall.FdSet
	var err *hyper.Error
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
					err = (*hyper.Error)(task.Value())
					Fail(err)
				}
				if task.Type() != hyper.TaskClientConn {
					c.Printf(c.Str("unexpected task type\n"))
					Fail(err)
				}

				client := (*hyper.ClientConn)(task.Value())
				task.Free()

				// Send it!
				sendTask := client.Send(req)
				SetUserData(sendTask, hyper.ExampleSend)
				sendRes := exec.Push(sendTask)
				if sendRes != hyper.OK {
					panic("error send\n")
				}

				// For this example, no longer need the client
				client.Free()

				break
			case hyper.ExampleSend:
				if task.Type() == hyper.TaskError {
					c.Printf(c.Str("send error!\n"))
					err = (*hyper.Error)(task.Value())
					Fail(err)
				}
				if task.Type() != hyper.TaskResponse {
					c.Printf(c.Str("unexpected task type\n"))
					Fail(err)
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

				foreachTask := respBody.Foreach(AppendToResponseBody, c.Pointer(&response))

				SetUserData(foreachTask, hyper.ExampleRespBody)
				exec.Push(foreachTask)

				// No longer need the response
				resp.Free()

				break
			case hyper.ExampleRespBody:
				if task.Type() == hyper.TaskError {
					c.Printf(c.Str("body error!\n"))
					err = (*hyper.Error)(task.Value())
					Fail(err)
				}
				if task.Type() != hyper.TaskEmpty {
					c.Printf(c.Str("unexpected task type\n"))
					Fail(err)
				}

				// Cleaning up before exiting
				task.Free()
				exec.Free()
				FreeConnData(connData)

				if response.respBodyWriter != nil {
					defer response.respBodyWriter.Close()
				}

				return &response
			case hyper.ExampleNotSet:
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

		// Set the default request timeout
		var tv syscall.Timeval
		tv.Sec = 10

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

// ConnectTo connects to a host and port
func ConnectTo(host string, port string) c.Int {
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

	if os.Fcntl(sfd, os.F_SETFL, os.O_NONBLOCK) != 0 {
		panic("failed to set net to non-blocking\n")
	}
	return sfd
}

// ReadCallBack is the callback for reading from a socket
func ReadCallBack(userdata c.Pointer, ctx *hyper.Context, buf *uint8, bufLen uintptr) uintptr {
	conn := (*ConnData)(userdata)

	ret := os.Read(conn.Fd, c.Pointer(buf), bufLen)

	if ret >= 0 {
		return uintptr(ret)
	}

	if os.Errno != os.EAGAIN {
		c.Perror(c.Str("[read callback fail]"))
		// kaboom
		return hyper.IoError
	}

	// would block, register interest
	if conn.ReadWaker != nil {
		conn.ReadWaker.Free()
	}
	conn.ReadWaker = ctx.Waker()
	return hyper.IoPending
}

// WriteCallBack is the callback for writing to a socket
func WriteCallBack(userdata c.Pointer, ctx *hyper.Context, buf *uint8, bufLen uintptr) uintptr {
	conn := (*ConnData)(userdata)
	ret := os.Write(conn.Fd, c.Pointer(buf), bufLen)

	if int(ret) >= 0 {
		return uintptr(ret)
	}

	if os.Errno != os.EAGAIN {
		c.Perror(c.Str("[write callback fail]"))
		// kaboom
		return hyper.IoError
	}

	// would block, register interest
	if conn.WriteWaker != nil {
		conn.WriteWaker.Free()
	}
	conn.WriteWaker = ctx.Waker()
	return hyper.IoPending
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
}

// Fail prints the error details and panics
func Fail(err *hyper.Error) {
	if err != nil {
		c.Printf(c.Str("error code: %d\n"), err.Code())
		// grab the error details
		var errBuf [256]c.Char
		errLen := err.Print((*uint8)(c.Pointer(&errBuf[:][0])), uintptr(len(errBuf)))

		c.Printf(c.Str("details: %.*s\n"), c.Int(errLen), c.Pointer(&errBuf[:][0]))
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

// NewConnData creates a new connection data
func NewConnData(fd c.Int) *ConnData {
	return &ConnData{Fd: fd, ReadWaker: nil, WriteWaker: nil}
}

// NewIoWithConnReadWrite creates a new IO with read and write callbacks
func NewIoWithConnReadWrite(connData *ConnData) *hyper.Io {
	io := hyper.NewIo()
	io.SetUserdata(c.Pointer(connData))
	io.SetRead(ReadCallBack)
	io.SetWrite(WriteCallBack)
	return io
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
