package main

import (
	"github.com/goplus/llgo/c"
	"github.com/goplus/llgoexamples/c/fcntl"
	"github.com/goplus/llgoexamples/c/fddef"
	"github.com/goplus/llgoexamples/c/netdb"
	"github.com/goplus/llgoexamples/c/os"
	_select "github.com/goplus/llgoexamples/c/select"
	"github.com/goplus/llgoexamples/c/socket"
	"github.com/goplus/llgoexamples/rust/hyper"
	"unsafe"
)

type ConnData struct {
	Fd         c.Int
	ReadWaker  *hyper.Waker
	WriteWaker *hyper.Waker
}

const (
	ExampleNotSet = iota
	ExampleHandshake
	ExampleSend
	ExampleRespBody
)

func ReadCb(userdata c.Pointer, ctx *hyper.Context, buf *uint8, bufLen uintptr) uintptr {
	conn := (*ConnData)(userdata)

	// TODO 这里c-demo中的read是unistd.read，返回的是c.Long
	ret := os.Read(conn.Fd, c.Pointer(buf), bufLen)

	if ret >= 0 {
		return uintptr(ret)
	}

	// TODO 无法拿到C中的全局 errno
	//if errno != syscall.EAGAIN {
	//	// kaboom
	//	return hyper.IoError
	//}

	// would block, register interest
	if conn.ReadWaker != nil {
		conn.ReadWaker.Free()
	}
	conn.ReadWaker = ctx.Waker()
	return hyper.IoPending
}

func WriteCb(userdata c.Pointer, ctx *hyper.Context, buf *uint8, bufLen uintptr) uintptr {
	conn := (*ConnData)(userdata)
	// TODO
	ret := os.Write(conn.Fd, c.Pointer(buf), bufLen)

	if int(ret) >= 0 {
		return uintptr(ret)
	}

	// TODO 无法拿到C中的全局 errno
	//if errno != syscall.EAGAIN {
	//	// kaboom
	//	return hyper.IoError
	//}

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
	// TODO
	var hints netdb.AddrInfo
	c.Memset(c.Pointer(&hints), 0, unsafe.Sizeof(netdb.AddrInfo{}))
	hints.AiFamily = socket.AF_UNSPEC
	hints.AiSockType = socket.SOCK_STREAM

	var result, rp *netdb.AddrInfo
	if netdb.GetAddrInfo(host, port, &hints, &result) != 0 {
		c.Printf(c.Str("dns failed for %s\n"), host)
		return -1
	}

	var sfd c.Int
	for rp = result; rp != nil; rp = rp.AiNext {
		sfd = socket.Socket(rp.AiFamily, rp.AiSockType, rp.AiSockType)
		if sfd == -1 {
			continue
		}
		if socket.Connect(sfd, rp.AiAddr, rp.AiAddrLen) != -1 {
			break
		}
		os.Close(sfd)
	}

	netdb.FreeAddrInfo(result)

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

	// TODO
	os.Read(1, c.Pointer(buf), len)

	return hyper.IterContinue
}

func fail(err *hyper.Error) {
	if err != nil {
		c.Printf(c.Str("error code: %d\n"), err.Code())
		// grab the error details
		var errBuf [256]c.Char
		errLen := err.Print((*uint8)(c.Pointer(&errBuf[0])), uintptr(len(errBuf)))
		c.Printf(c.Str("details: %.*s\n"), int(errLen), errBuf)

		// clean up the error
		err.Free()
	}
	return
}

func main() {
	host := "httpbin.org"
	port := "80"
	path := "/"
	c.Printf(c.Str("connecting to port %s on %s...\n"), port, host)

	fd := ConnectTo(c.Str(host), c.Str(port))

	if fd < 0 {
		return
	}

	c.Printf(c.Str("connected to %s, now get %s\n"), host, path)
	if fcntl.FcNtl(fd, fcntl.F_SETFL, fcntl.O_NONBLOCK) != 0 {
		c.Printf(c.Str("failed to set socket to non-blocking\n"))
		return
	}

	var fdsRead, fdsWrite, fdsExcep fddef.FdSet

	//conn := &ConnData{Fd: c.Int(fd), ReadWaker: nil, WriteWaker: nil}
	conn := (*ConnData)(c.Malloc(unsafe.Sizeof(ConnData{})))
	conn.Fd = fd
	conn.ReadWaker = nil
	conn.WriteWaker = nil

	// Hookup the IO
	io := hyper.NewIo()
	io.SetUserdata(c.Pointer(conn))
	io.SetRead(ReadCb)
	io.SetWrite(WriteCb)

	c.Printf(c.Str("http handshake (hyper_client v%s) ...\n"), hyper.Version())

	// We need an executor generally to poll futures
	exec := hyper.NewExecutor()

	// Prepare client options
	opts := hyper.NewClientConnOptions()
	opts.Exec(exec)

	handshake := hyper.Handshake(io, opts)
	handshake.SetUserdata(c.Pointer(uintptr(ExampleHandshake)))

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

			// TODO
			switch int(uintptr(task.Userdata())) {
			case ExampleHandshake:
				if task.Type() == hyper.TaskError {
					c.Printf(c.Str("handshake error!\n"))
					err = (*hyper.Error)(task.Value())
					fail(err)
				}
				if task.Type() != hyper.TaskClientConn {
					c.Printf(c.Str("unexpected task type\n"))
					fail(err)
				}

				c.Printf(c.Str("preparing http request ...\n"))

				client := (*hyper.ClientConn)(task.Value())
				task.Free()

				// Prepare the request
				req := hyper.NewRequest()

				//if req.SetMethod((*uint8)(c.Pointer(&[]byte("GET")[0])), uintptr(len("GET"))) != hyper.OK {
				if req.SetMethod((*uint8)(c.Pointer(c.Str("GET"))), uintptr(len("GET"))) != hyper.OK {
					c.Printf(c.Str("error setting method\n"))
					return
				}
				if req.SetURI((*uint8)(c.Pointer(c.Str(path))), uintptr(len(path))) != hyper.OK {
					c.Printf(c.Str("error setting uri\n"))
					return
				}

				req_headers := req.Headers()
				req_headers.Set((*uint8)(c.Pointer(c.Str("Host"))), uintptr(len("Host")), (*uint8)(c.Pointer(c.Str(host))), uintptr(len(host)))

				// Send it!
				send := client.Send(req)
				send.SetUserdata(c.Pointer(uintptr(ExampleSend)))
				c.Printf(c.Str("sending ...\n"))
				exec.Push(send)

				// For this example, no longer need the client
				client.Free()

				break
			case ExampleSend:
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

				http_status := resp.Status()
				rp := resp.ReasonPhrase()
				rp_len := resp.ReasonPhraseLen()

				c.Printf(c.Str("\nResponse Status: %d %.*s\n"), http_status, rp_len, rp)

				headers := resp.Headers()
				headers.Foreach(PrintEachHeader, nil)
				c.Printf(c.Str("\n"))

				resp_body := resp.Body()
				foreach := resp_body.Foreach(PrintEachChunk, nil)
				foreach.SetUserdata(c.Pointer(uintptr(ExampleRespBody)))
				exec.Push(foreach)

				// No longer need the response
				resp.Free()

				break
			case ExampleRespBody:
				if task.Type() == hyper.TaskError {
					c.Printf(c.Str("body error!\n"))
					err = (*hyper.Error)(task.Value())
					fail(err)
				}
				if task.Type() != hyper.TaskEmpty {
					c.Printf(c.Str("unexpected task type\n"))
					fail(err)
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
		}

		// All futures are pending on IO work, so select on the fds.

		fddef.FdZero(&fdsRead)
		fddef.FdZero(&fdsWrite)
		fddef.FdZero(&fdsExcep)

		// TODO
		if conn.ReadWaker != nil {
			fddef.DarwinFdSet(conn.Fd, &fdsRead)
		}
		if conn.WriteWaker != nil {
			fddef.DarwinFdSet(conn.Fd, &fdsWrite)
		}

		err2 := _select.Select(int(conn.Fd+1), &fdsRead, &fdsWrite, &fdsExcep, nil)
		if err2 != nil {
			c.Printf(c.Str("select() error"))
			return
		}

		if fddef.DarwinFdIsSet(conn.Fd, &fdsRead) != 0 {
			conn.ReadWaker.Wake()
			conn.ReadWaker = nil
		}

		if fddef.DarwinFdIsSet(conn.Fd, &fdsWrite) != 0 {
			conn.WriteWaker.Wake()
			conn.WriteWaker = nil
		}
	}
}
