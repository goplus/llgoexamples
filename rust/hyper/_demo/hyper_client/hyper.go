package main

import (
	"fmt"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/net"
	"github.com/goplus/llgo/c/os"
	"github.com/goplus/llgo/c/sys"
	"github.com/goplus/llgo/c/syscall"
	"github.com/goplus/llgoexamples/rust/hyper"
)

func main() {
	resp := Get("httpbin.org", "80", "/")
	fmt.Println(resp.Status, resp.Message)
	resp.PrintBody()
}

type ConnData struct {
	Fd         c.Int
	ReadWaker  *hyper.Waker
	WriteWaker *hyper.Waker
}

type Response struct {
	Status          uint16
	Message         string
	ResponseBody    *uint8
	ResponseBodyLen uintptr
}

// Get Send a Get http request
func Get(host, port, path string) *Response {
	req := hyper.NewRequest()

	// Prepare the request
	// Set the request method and uri
	if req.SetMethod((*uint8)(&[]byte("GET")[0]), c.Strlen(c.Str("GET"))) != hyper.OK {
		panic(fmt.Sprintf("error setting method %s\n", "GET"))
	}
	if req.SetURI((*uint8)(&[]byte(path)[0]), c.Strlen(c.AllocaCStr(path))) != hyper.OK {
		panic(fmt.Sprintf("error setting uri %s\n", path))
	}

	// Set the request headers
	reqHeaders := req.Headers()
	if reqHeaders.Set((*uint8)(&[]byte("Host")[0]), c.Strlen(c.Str("Host")), (*uint8)(&[]byte(host)[0]), c.Strlen(c.AllocaCStr(host))) != hyper.OK {
		panic("error setting headers\n")
	}

	//var response RequestResponse

	fd := ConnectTo(host, port)

	connData := &ConnData{Fd: fd, ReadWaker: nil, WriteWaker: nil}

	// Hookup the IO
	io := hyper.NewIo()
	io.SetUserdata(c.Pointer(connData))
	io.SetRead(ReadCallBack)
	io.SetWrite(WriteCallBack)

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
					fail(err)
				}
				if task.Type() != hyper.TaskClientConn {
					c.Printf(c.Str("169 unexpected task type\n"))
					fail(err)
				}

				c.Printf(c.Str("preparing http request ...\n"))

				client := (*hyper.ClientConn)(task.Value())
				task.Free()

				// Send it!
				sendTask := client.Send(req)
				SetUserData(sendTask, hyper.ExampleSend)
				c.Printf(c.Str("sending ...\n"))
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
					fail(err)
				}
				if task.Type() != hyper.TaskResponse {
					c.Printf(c.Str("unexpected task type\n"))
					fail(err)
				}

				// Take the results
				resp := (*hyper.Response)(task.Value())
				task.Free()

				rp := resp.ReasonPhrase()
				rpLen := resp.ReasonPhraseLen()

				response.Status = resp.Status()
				response.Message = string((*[1 << 30]byte)(c.Pointer(rp))[:rpLen:rpLen])

				headers := resp.Headers()
				headers.Foreach(PrintEachHeader, nil)
				c.Printf(c.Str("\n"))

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
					fail(err)
				}
				if task.Type() != hyper.TaskEmpty {
					c.Printf(c.Str("unexpected task type\n"))
					fail(err)
				}

				// Cleaning up before exiting
				task.Free()
				exec.Free()
				FreeConnData(connData)

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

		// Set the request timeout
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

// ConnectTo Connect to a host
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

// ReadCallBack Read callback
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

// WriteCallBack Write callback
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

// PrintEachHeader HeaderForeachCallback function: Process the response headers
func PrintEachHeader(userdata c.Pointer, name *uint8, nameLen uintptr, value *uint8, valueLen uintptr) c.Int {
	c.Printf(c.Str("%.*s: %.*s\n"), int(nameLen), name, int(valueLen), value)
	return hyper.IterContinue
}

// AppendToResponseBody BodyForeachCallback function: Process the response body
func AppendToResponseBody(userdata c.Pointer, chunk *hyper.Buf) c.Int {
	resp := (*Response)(userdata)
	buf := chunk.Bytes()
	len := chunk.Len()
	responseBody := (*uint8)(c.Malloc(resp.ResponseBodyLen + len))
	if responseBody == nil {
		c.Fprintf(c.Stderr, c.Str("Failed to allocate memory for response body\n"))
		return hyper.IterBreak
	}

	// Copy the existing response body to the new buffer
	if resp.ResponseBody != nil {
		c.Memcpy(c.Pointer(responseBody), c.Pointer(resp.ResponseBody), resp.ResponseBodyLen)
		c.Free(c.Pointer(resp.ResponseBody))
	}

	// Append the new data
	c.Memcpy(c.Pointer(uintptr(c.Pointer(responseBody))+resp.ResponseBodyLen), c.Pointer(buf), len)
	resp.ResponseBody = responseBody
	resp.ResponseBodyLen += len
	return hyper.IterContinue
}

// PrintBody Print the response body
func (resp *Response) PrintBody() {
	//c.Printf(c.Str("%.*s\n"), c.Int(resp.ResponseBodyLen), resp.ResponseBody)
	fmt.Println(string((*[1 << 30]byte)(c.Pointer(resp.ResponseBody))[:resp.ResponseBodyLen:resp.ResponseBodyLen]))
}

// FreeConnData Free the connection data
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

// SetUserData Set the userdata
func SetUserData(task *hyper.Task, userData hyper.ExampleId) {
	// Constant cannot be converted to pointer
	var data = userData
	task.SetUserdata(c.Pointer(uintptr(data)))
}

// fail Fail the request
func fail(err *hyper.Error) {
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
