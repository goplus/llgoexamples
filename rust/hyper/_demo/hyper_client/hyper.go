package main

import (
	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/fddef"
	"github.com/goplus/llgo/c/netdb"
	"github.com/goplus/llgo/c/os"
	_select "github.com/goplus/llgo/c/select"
	"github.com/goplus/llgo/c/socket"
	"github.com/goplus/llgoexamples/rust/hyper"
)

type ConnData struct {
	Fd         c.Int
	ReadWaker  *hyper.Waker
	WriteWaker *hyper.Waker
}

type ExampleId c.Int

const (
	ExampleNotSet ExampleId = iota
	ExampleHandshake
	ExampleSend
	ExampleRespBody
)

func ReadCallBack(userdata c.Pointer, ctx *hyper.Context, buf *uint8, bufLen uintptr) uintptr {
	conn := (*ConnData)(userdata)

	ret := os.Read(conn.Fd, c.Pointer(buf), bufLen)

	if ret >= 0 {
		return uintptr(ret)
	}

	if os.Errno != 35 {
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

func WriteCallBack(userdata c.Pointer, ctx *hyper.Context, buf *uint8, bufLen uintptr) uintptr {
	conn := (*ConnData)(userdata)
	ret := os.Write(conn.Fd, c.Pointer(buf), bufLen)

	if int(ret) >= 0 {
		return uintptr(ret)
	}

	print("errno == ", os.Errno)
	if os.Errno != 35 {
		c.Perror(c.Str("[read callback fail]"))
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

func ConnectTo(host *c.Char, port *c.Char) c.Int {
	var hints netdb.AddrInfo
	//c.Memset(c.Pointer(&hints), 0, unsafe.Sizeof(netdb.AddrInfo{}))
	hints.AiFamily = socket.AF_UNSPEC
	hints.AiSockType = socket.SOCK_STREAM

	var result, rp *netdb.AddrInfo
	if netdb.Getaddrinfo(host, port, &hints, &result) != 0 {
		c.Printf(c.Str("dns failed for %s\n"), host)
		return -1
	}

	var sfd c.Int
	for rp = result; rp != nil; rp = rp.AiNext {
		sfd = socket.Socket(rp.AiFamily, rp.AiSockType, rp.AiProtocol)
		if sfd == -1 {
			continue
		}
		if socket.Connect(sfd, rp.AiAddr, rp.AiAddrLen) != -1 {
			break
		}
		os.Close(sfd)
	}

	netdb.Freeaddrinfo(result)

	// no address succeeded
	if rp == nil {
		c.Printf(c.Str("connect failed for %s\n"), host)
		return -1
	}
	return sfd
}

func PrintEachHeader(userdata c.Pointer, name *uint8, nameLen uintptr, value *uint8, valueLen uintptr) c.Int {
	c.Printf(c.Str("%.*s: %.*s\n"), int(nameLen), name, int(valueLen), value)
	return hyper.IterContinue
}

func PrintEachChunk(userdata c.Pointer, chunk *hyper.Buf) c.Int {
	buf := chunk.Bytes()
	len := chunk.Len()

	os.Write(1, c.Pointer(buf), len)

	return hyper.IterContinue
}

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
	}
	return
}

func main() {
	host := "httpbin.org"
	port := "80"
	path := "/"
	c.Printf(c.Str("connecting to port %s on %s...\n"), c.Str(port), c.Str(host))

	fd := ConnectTo(c.Str(host), c.Str(port))

	if fd < 0 {
		return
	}

	c.Printf(c.Str("connected to %s, now get %s\n"), c.Str(host), c.Str(path))

	if os.Fcntl(fd, os.F_SETFL, os.O_NONBLOCK) != 0 {
		c.Printf(c.Str("failed to set socket to non-blocking\n"))
		return
	}

	var fdsRead, fdsWrite, fdsExcep fddef.FdSet

	conn := &ConnData{Fd: fd, ReadWaker: nil, WriteWaker: nil}

	// Hookup the IO
	io := hyper.NewIo()
	io.SetUserdata(c.Pointer(conn))
	io.SetRead(ReadCallBack)
	io.SetWrite(WriteCallBack)

	c.Printf(c.Str("http handshake (hyper_client v%s) ...\n"), hyper.Version())

	// We need an executor generally to poll futures
	exec := hyper.NewExecutor()

	// Prepare client options
	opts := hyper.NewClientConnOptions()
	opts.Exec(exec)

	handshake := hyper.Handshake(io, opts)
	var exampleHandshake = ExampleHandshake
	handshake.SetUserdata(c.Pointer(uintptr(exampleHandshake)))

	// Let's wait for the handshake to finish...
	exec.Push(handshake)

	var err *hyper.Error

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
				if task.Type() == hyper.TaskError {
					c.Printf(c.Str("handshake error!\n"))
					err = (*hyper.Error)(task.Value())
					fail(err)
					return
				}
				if task.Type() != hyper.TaskClientConn {
					c.Printf(c.Str("unexpected task type\n"))
					fail(err)
					return
				}

				c.Printf(c.Str("preparing http request ...\n"))

				client := (*hyper.ClientConn)(task.Value())
				task.Free()

				// Prepare the request
				req := hyper.NewRequest()

				if req.SetMethod((*uint8)(&[]byte("GET")[0]), c.Strlen(c.Str("GET"))) != hyper.OK {
					c.Printf(c.Str("error setting method\n"))
					return
				}
				if req.SetURI((*uint8)(&[]byte(path)[0]), c.Strlen(c.Str(path))) != hyper.OK {
					c.Printf(c.Str("error setting uri\n"))
					return
				}

				reqHeaders := req.Headers()
				if reqHeaders.Set((*uint8)(&[]byte("Host")[0]), c.Strlen(c.Str("Host")), (*uint8)(&[]byte(host)[0]), c.Strlen(c.Str(host))) != hyper.OK {
					c.Printf(c.Str("error setting headers\n"))
					return
				}

				// Send it!
				send := client.Send(req)
				var exampleSend = ExampleSend
				send.SetUserdata(c.Pointer(uintptr(exampleSend)))
				c.Printf(c.Str("sending ...\n"))
				sendRes := exec.Push(send)
				if sendRes != hyper.OK {
					c.Printf(c.Str("error send\n"))
					return
				}

				// For this example, no longer need the client
				client.Free()

				break
			case ExampleSend:
				println("ExampleSend")
				if task.Type() == hyper.TaskError {
					c.Printf(c.Str("send error!\n"))
					err = (*hyper.Error)(task.Value())
					fail(err)
					return
				}
				if task.Type() != hyper.TaskResponse {
					c.Printf(c.Str("unexpected task type\n"))
					fail(err)
					return
				}

				// Take the results
				resp := (*hyper.Response)(task.Value())
				task.Free()

				httpStatus := resp.Status()
				rp := resp.ReasonPhrase()
				rpLen := resp.ReasonPhraseLen()

				c.Printf(c.Str("\nResponse Status: %d %.*s\n"), httpStatus, rpLen, rp)

				headers := resp.Headers()
				headers.Foreach(PrintEachHeader, nil)
				c.Printf(c.Str("\n"))

				respBody := resp.Body()
				foreach := respBody.Foreach(PrintEachChunk, nil)
				var exampleRespBody = ExampleRespBody
				foreach.SetUserdata(c.Pointer(uintptr(exampleRespBody)))
				exec.Push(foreach)

				// No longer need the response
				resp.Free()

				break
			case ExampleRespBody:
				if task.Type() == hyper.TaskError {
					c.Printf(c.Str("body error!\n"))
					err = (*hyper.Error)(task.Value())
					fail(err)
					return
				}
				if task.Type() != hyper.TaskEmpty {
					c.Printf(c.Str("unexpected task type\n"))
					fail(err)
					return
				}

				c.Printf(c.Str("\n -- Done! -- \n"))

				// Cleaning up before exiting
				task.Free()
				exec.Free()
				FreeConnData(conn)

				return
			case ExampleNotSet:
				// A background task for hyper_client completed...
				task.Free()
				break
			}
			//break
		}

		// All futures are pending on IO work, so select on the fds.

		fddef.FdZero(&fdsRead)
		fddef.FdZero(&fdsWrite)
		fddef.FdZero(&fdsExcep)

		if conn.ReadWaker != nil {
			fddef.Fdset(conn.Fd, &fdsRead)
		}
		if conn.WriteWaker != nil {
			fddef.Fdset(conn.Fd, &fdsWrite)
		}

		var tv _select.TimeVal
		tv.TvSec = 10

		selRet := _select.Select(conn.Fd+1, &fdsRead, &fdsWrite, &fdsExcep, &tv)
		if selRet < 0 {
			c.Printf(c.Str("select() error\n"))
			return
		} else if selRet == 0 {
			c.Printf(c.Str("select() timeout\n"))
			return
		}

		if fddef.FdIsset(conn.Fd, &fdsRead) != 0 {
			conn.ReadWaker.Wake()
			conn.ReadWaker = nil
		}

		if fddef.FdIsset(conn.Fd, &fdsWrite) != 0 {
			conn.WriteWaker.Wake()
			conn.WriteWaker = nil
		}
	}
}
