package main

import (
	"fmt"
	"os"
	"unsafe"
	"sync/atomic"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/libuv"
	"github.com/goplus/llgo/c/net"
	cos "github.com/goplus/llgo/c/os"
	"github.com/goplus/llgo/c/syscall"
	
	"github.com/goplus/llgoexamples/rust/hyper"
)

const (
	MAX_EVENTS = 128
)

var (
	exec                        *hyper.Executor
	http1Opts                   *hyper.Http1ServerconnOptions
	http2Opts                   *hyper.Http2ServerconnOptions
	loop                        *libuv.Loop
	server                      libuv.Tcp
	checkHandle                 libuv.Check
	sigintHandle, sigtermHandle libuv.Signal             
	shouldExit                  atomic.Bool
)

type ConnData struct {
	Stream       libuv.Tcp
	PollHandle   libuv.Poll
	EventMask    c.Uint
	ReadWaker    *hyper.Waker
	WriteWaker   *hyper.Waker
    IsClosing   atomic.Bool
    ClosedHandles int32
}

type ServiceUserdata struct {
	Host     [128]c.Char
	Port     [8]c.Char
	Executor *hyper.Executor
}

func onSignal(handle *libuv.Signal, sigNum c.Int) {
	fmt.Printf("Caught signal %d... exiting\n", sigNum)
	shouldExit.Store(true)
	sigintHandle.Stop()
	sigtermHandle.Stop()
	(*libuv.Handle)(unsafe.Pointer(&server)).Close(nil)
	loop.Close()
}

func closeWalkCb(handle *libuv.Handle, arg c.Pointer) {
	if handle.IsClosing() == 0 {
		handle.Close(nil)
	}
}


func onPoll(handle *libuv.Poll, status c.Int, events c.Int) {
	conn := (*ConnData)((*libuv.Handle)(unsafe.Pointer(handle)).GetData())

	if status < 0 {
		fmt.Fprintf(os.Stderr, "Poll error: %s\n", libuv.Strerror(libuv.Errno(status)))
		return
	}

	if events&c.Int(libuv.READABLE) != 0 && conn.ReadWaker != nil {
		conn.ReadWaker.Wake()
		conn.ReadWaker = nil
	}

	if events&c.Int(libuv.WRITABLE) != 0 && conn.WriteWaker != nil {
		conn.WriteWaker.Wake()
		conn.WriteWaker = nil
	}
}

func updateConnDataRegistrations(conn *ConnData, create bool) bool {
	events := c.Int(0)
	if conn.EventMask&c.Uint(libuv.READABLE) != 0 {
		events |= c.Int(libuv.READABLE)
	}
	if conn.EventMask&c.Uint(libuv.WRITABLE) != 0 {
		events |= c.Int(libuv.WRITABLE)
	}

	r := conn.PollHandle.Start(events, onPoll)
	if r < 0 {
		fmt.Fprintf(os.Stderr, "uv_poll_start error: %s\n", libuv.Strerror(libuv.Errno(r)))
		return false
	}
	return true
}

func readCb(userdata c.Pointer, ctx *hyper.Context, buf *byte, bufLen uintptr) uintptr {
	conn := (*ConnData)(userdata)
	ret := net.Recv(conn.Stream.GetIoWatcherFd(), unsafe.Pointer(buf), bufLen, 0)

	if ret >= 0 {
		return uintptr(ret)
	}

	if uintptr(cos.Errno) != syscall.EAGAIN && uintptr(cos.Errno) != syscall.EWOULDBLOCK {
		return hyper.IoError
	}

	if conn.ReadWaker != nil {
		conn.ReadWaker.Free()
	}

	if conn.EventMask&c.Uint(libuv.READABLE) == 0 {
		conn.EventMask |= c.Uint(libuv.READABLE)
		if !updateConnDataRegistrations(conn, false) {
			return hyper.IoError
		}
	}

	conn.ReadWaker = ctx.Waker()
	return hyper.IoPending
}

func writeCb(userdata c.Pointer, ctx *hyper.Context, buf *byte, bufLen uintptr) uintptr {
	conn := (*ConnData)(userdata)
	ret := net.Send(conn.Stream.GetIoWatcherFd(), unsafe.Pointer(buf), bufLen, 0)

	if ret >= 0 {
		return uintptr(ret)
	}

	if uintptr(cos.Errno) != syscall.EAGAIN && uintptr(cos.Errno) != syscall.EWOULDBLOCK {
		return hyper.IoError
	}

	if conn.WriteWaker != nil {
		conn.WriteWaker.Free()
	}

	if conn.EventMask&c.Uint(libuv.WRITABLE) == 0 {
		conn.EventMask |= c.Uint(libuv.WRITABLE)
		if !updateConnDataRegistrations(conn, false) {
			return hyper.IoError
		}
	}

	conn.WriteWaker = ctx.Waker()
	return hyper.IoPending
}

func createConnData() (*ConnData, error) {
	conn := &ConnData{}
	if conn == nil {
		return nil, fmt.Errorf("failed to allocate conn_data")
	}
	conn.IsClosing.Store(false)
	conn.ClosedHandles = 0

	return conn, nil
}

func freeConnData(userdata c.Pointer) {
	conn := (*ConnData)(userdata)
	if conn != nil && !conn.IsClosing.Swap(true){
		fmt.Printf("Closing connection...\n")
		if conn.ReadWaker != nil {
			conn.ReadWaker.Free()
			conn.ReadWaker = nil
		}
		if conn.WriteWaker != nil {
			conn.WriteWaker.Free()
			conn.WriteWaker = nil
		}

		if (*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).IsClosing() == 0 {
            (*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).Close(nil)
        }

        if (*libuv.Handle)(unsafe.Pointer(&conn.Stream)).IsClosing() == 0 {
            (*libuv.Handle)(unsafe.Pointer(&conn.Stream)).Close(nil)
        }
	}
}

func createIo(conn *ConnData) *hyper.Io {
	io := hyper.NewIo()
	io.SetUserdata(unsafe.Pointer(conn), freeConnData)
	io.SetRead(readCb)
	io.SetWrite(writeCb)

	return io
}

func createServiceUserdata() *ServiceUserdata {
	userdata := &ServiceUserdata{}
    if userdata == nil {
        fmt.Fprintf(os.Stderr, "Failed to allocate service_userdata\n")
    }
    return userdata
}

func printEachHeader(userdata c.Pointer, name *byte, nameLen uintptr, value *byte, valueLen uintptr) c.Int {
	fmt.Printf("%.*s: %.*s\n", int(nameLen), c.GoString((*c.Char)(c.Pointer(name))),
		int(valueLen), c.GoString((*c.Char)(unsafe.Pointer(value))))
	return hyper.IterContinue
}

func printBodyChunk(userdata c.Pointer, chunk *hyper.Buf) c.Int {
	buf := chunk.Bytes()
	len := chunk.Len()
	cos.Write(1, unsafe.Pointer(buf), len)

	return hyper.IterContinue
}

func sendEachBodyChunk(userdata c.Pointer, ctx *hyper.Context, chunk **hyper.Buf) c.Int {
	chunkCount := (*c.Int)(userdata)
	if *chunkCount > 0 {
		var data [64]c.Char
		c.Snprintf((*c.Char)(&data[0]), unsafe.Sizeof(data), c.Str("Chunk %d\n"), *chunkCount)
		*chunk = hyper.CopyBuf((*byte)(unsafe.Pointer(&data[0])), c.Strlen((*c.Char)(&data[0])))
		*chunkCount--
		return hyper.PollReady
	} else {
		*chunk = nil
		return hyper.PollReady
	}
}

func serverCallback(userdata c.Pointer, request *hyper.Request, channel *hyper.ResponseChannel) {
	serviceData := (*ServiceUserdata)(userdata)

	fmt.Printf("Handling request on connection from %s:%s\n",
	c.GoString((*c.Char)(&serviceData.Host[0])), c.GoString((*c.Char)(&serviceData.Port[0])))

	if request == nil {
		fmt.Fprintf(os.Stderr, "Error: Received null request\n")
		return
	}

	var scheme [64]byte
	var authority [256]byte
	var pathAndQuery [1024]byte
	schemeLen := unsafe.Sizeof(scheme)
	authorityLen := unsafe.Sizeof(authority)
	pathAndQueryLen := unsafe.Sizeof(pathAndQuery)

	uriResult := request.URIParts(&scheme[0], &schemeLen, &authority[0], &authorityLen, &pathAndQuery[0], &pathAndQueryLen)
	if uriResult == hyper.OK {
		fmt.Printf("Scheme: %s\n", string(scheme[:schemeLen]))
		fmt.Printf("Authority: %s\n", string(authority[:authorityLen]))
		fmt.Printf("Path and Query: %s\n", string(pathAndQuery[:pathAndQueryLen]))
	} else {
		fmt.Fprintf(os.Stderr, "Failed to get URI parts. Error code: %d\n", uriResult)
	}

	version := request.Version()
	fmt.Printf("HTTP Version: ")
	switch version {
	case hyper.HTTPVersionNone:
		fmt.Println("None")
	case hyper.HTTPVersion10:
		fmt.Println("HTTP/1.0")
	case hyper.HTTPVersion11:
		fmt.Println("HTTP/1.1")
	case hyper.HTTPVersion2:
		fmt.Println("HTTP/2")
	default:
		fmt.Printf("Unknown (%d)\n", version)
	}

	var method [32]byte
	methodLen := unsafe.Sizeof(method)
	methodResult := request.Method(&method[0], &methodLen)
	if methodResult == hyper.OK {
		fmt.Printf("Method: %s\n", string(method[:methodLen]))
	} else {
		fmt.Fprintf(os.Stderr, "Failed to get request method. Error code: %d\n", methodResult)
	}

	fmt.Println("Headers:")
	reqHeaders := request.Headers()
	if reqHeaders != nil {
		reqHeaders.Foreach(printEachHeader, nil)
	} else {
		fmt.Fprintf(os.Stderr, "Error: Failed to get request headers\n")
	}

	if methodLen > 0 && (c.Strncmp((*c.Char)(unsafe.Pointer(&method[0])), c.Str("POST"), methodLen) == 0 ||
		c.Strncmp((*c.Char)(unsafe.Pointer(&method[0])), c.Str("PUT"), methodLen) == 0) {
		fmt.Println("Request Body:")
		body := request.Body()
		if body != nil {
			task := body.Foreach(printBodyChunk, nil, nil)
			if task != nil {
				r := serviceData.Executor.Push(task)
				if r != hyper.OK {
					fmt.Fprintf(os.Stderr, "Error: Failed to push body foreach task\n")
					task.Free()
				}
			} else {
				fmt.Fprintf(os.Stderr, "Error: Failed to create body foreach task\n")
			}
		} else {
			fmt.Fprintf(os.Stderr, "Error: Failed to get request body\n")
		}
	}

	response := hyper.NewResponse()
	if response != nil {
		response.SetStatus(200)
		rspHeaders := response.Headers()
		if rspHeaders != nil {
			hres := rspHeaders.Set((*byte)(unsafe.Pointer(c.Str("Content-Type"))), uintptr(12), (*byte)(unsafe.Pointer(c.Str("text/plain"))), uintptr(10))
			if hres != hyper.OK {
				fmt.Fprintf(os.Stderr, "Error: Failed to set response headers\n")
			}
			hres = rspHeaders.Set((*byte)(unsafe.Pointer(c.Str("Cache-Control"))), uintptr(13), (*byte)(unsafe.Pointer(c.Str("no-cache"))), uintptr(8))
			if hres != hyper.OK {
				fmt.Fprintf(os.Stderr, "Error: Failed to set response headers\n")
			}
		} else {
			fmt.Fprintf(os.Stderr, "Error: Failed to get response headers\n")
		}

		if methodLen > 0 && c.Strncmp((*c.Char)(unsafe.Pointer(&method[0])), c.Str("GET"), methodLen) == 0 {
			body := hyper.NewBody()
			if body != nil {
				body.SetDataFunc(sendEachBodyChunk)
				chunkCount := (*c.Int)(c.Malloc(unsafe.Sizeof(c.Int(0))))
				if chunkCount != nil {
					*chunkCount = 10
					body.SetUserdata(unsafe.Pointer(chunkCount), func(p c.Pointer) { c.Free(p) })
					response.SetBody(body)
				} else {
					fmt.Fprintf(os.Stderr, "Error: Failed to allocate chunk_count\n")
				}
			} else {
				fmt.Fprintf(os.Stderr, "Error: Failed to create response body\n")
			}
		}

		channel.Send(response)
	} else {
		fmt.Fprintf(os.Stderr, "Error: Failed to create response\n")
	}

	// We don't close the connection here. Let hyper handle keep-alive.
	//free request
	request.Free()
}

func onNewConnection(serverStream *libuv.Stream, status c.Int) {
	if status < 0 {
		fmt.Fprintf(os.Stderr, "New connection error %s\n", libuv.Strerror(libuv.Errno(status)))
		return
	}

	conn, err := createConnData()
    if err != nil {
        fmt.Fprintf(os.Stderr, "Failed to create conn_data\n")
        return
    }

	libuv.InitTcp(loop, &conn.Stream)
	conn.Stream.Data = unsafe.Pointer(conn)

	if serverStream.Accept((*libuv.Stream)(unsafe.Pointer(&conn.Stream))) == 0 {
		r := libuv.PollInit(loop, &conn.PollHandle, libuv.OsFd(conn.Stream.GetIoWatcherFd()))
		if r < 0 {
			fmt.Fprintf(os.Stderr, "uv_poll_init error: %s\n", libuv.Strerror(libuv.Errno(r)))
			(*libuv.Handle)(unsafe.Pointer(&conn.Stream)).Close(nil)
			return
		}
	
		(*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).Data = unsafe.Pointer(conn)
	
		if !updateConnDataRegistrations(conn, true) {
			(*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).Close(nil)
			(*libuv.Handle)(unsafe.Pointer(&conn.Stream)).Close(nil)
			return
		}

		userdata := createServiceUserdata()
		if userdata == nil {
			fmt.Fprintf(os.Stderr, "Failed to create service_userdata\n")
			(*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).Close(nil)
			(*libuv.Handle)(unsafe.Pointer(&conn.Stream)).Close(nil)
			return
		}
		userdata.Executor = exec

		var addr net.SockaddrStorage
		addrlen := c.Int(unsafe.Sizeof(addr))
		conn.Stream.Getpeername((*net.SockAddr)(c.Pointer(&addr)), &addrlen)

		if addr.Family == net.AF_INET {
			s := (*net.SockaddrIn)(unsafe.Pointer(&addr))
			libuv.Ip4Name(s, (*c.Char)(&userdata.Host[0]), unsafe.Sizeof(userdata.Host))
			c.Snprintf((*c.Char)(&userdata.Port[0]), unsafe.Sizeof(userdata.Port), c.Str("%d"), net.Ntohs(s.Port))
		} else if addr.Family == net.AF_INET6 {
			s := (*net.SockaddrIn6)(unsafe.Pointer(&addr))
			libuv.Ip6Name(s, (*c.Char)(&userdata.Host[0]), unsafe.Sizeof(userdata.Host))
			c.Snprintf((*c.Char)(&userdata.Port[0]), unsafe.Sizeof(userdata.Port), c.Str("%d"), net.Ntohs(s.Port))
		}

		io := createIo(conn)

		service := hyper.ServiceNew(serverCallback)
		service.SetUserdata(unsafe.Pointer(userdata), nil)

		serverconn := hyper.ServeHttpXConnection(http1Opts, http2Opts, io, service)
		userdata.Executor.Push(serverconn)

	} else {
		(*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).Close(nil)
		(*libuv.Handle)(unsafe.Pointer(&conn.Stream)).Close(nil)
	}
}

func onCheck(handle *libuv.Check) {
	task := exec.Poll()
    for task != nil {
        taskType := task.Type()
        if taskType == hyper.TaskError {
            fmt.Println("hyper task failed with error!")

            err := (*hyper.Error)(task.Value())
            fmt.Printf("error code: %d\n", err.Code())
            
            var errbuf [256]byte
            errlen := err.Print(&errbuf[0], unsafe.Sizeof(errbuf))
            fmt.Printf("details: %s\n", errbuf[:errlen])

            err.Free()
            task.Free()
        } else if taskType == hyper.TaskEmpty {
            fmt.Println("internal hyper task complete")
            task.Free()
        } else if taskType == hyper.TaskServerconn {
            fmt.Println("server connection task complete")
            task.Free()
        }

		task = exec.Poll()
    }

    if shouldExit.Load() {
        fmt.Println("Shutdown initiated, cleaning up...")
        handle.Stop()
    }
}

func main() {
	shouldExit.Store(false)
	exec = hyper.NewExecutor()
	if exec == nil {
		fmt.Fprintf(os.Stderr, "Failed to create hyper executor\n")
		os.Exit(1)
	}

	http1Opts = hyper.Http1ServerconnOptionsNew(exec)
	if http1Opts == nil {
		fmt.Fprintf(os.Stderr, "Failed to create http1_opts\n")
		os.Exit(1)
	}
	result := http1Opts.HeaderReadTimeout(5 * 1000)
	if result != hyper.OK {
		fmt.Fprintf(os.Stderr, "Failed to set header read timeout for http1_opts\n")
		os.Exit(1)
	}

	http2Opts = hyper.Http2ServerconnOptionsNew(exec)
	if http2Opts == nil {
		fmt.Fprintf(os.Stderr, "Failed to create http2_opts\n")
		os.Exit(1)
	}
	result = http2Opts.KeepAliveInterval(5)
	if result != hyper.OK {
		fmt.Fprintf(os.Stderr, "Failed to set keep alive interval for http2_opts\n")
		os.Exit(1)
	}
	result = http2Opts.KeepAliveTimeout(5)
	if result != hyper.OK {
		fmt.Fprintf(os.Stderr, "Failed to set keep alive timeout for http2_opts\n")
		os.Exit(1)
	}

	host := "127.0.0.1"
	port := "1234"
	if len(os.Args) > 1 {
		host = os.Args[1]
	}
	if len(os.Args) > 2 {
		port = os.Args[2]
	}
	fmt.Printf("listening on port %s on %s...\n", port, host)

	loop = libuv.DefaultLoop()

	r := libuv.InitTcp(loop, &server)
	if r != 0 {
		fmt.Fprintf(os.Stderr, "Failed to initialize TCP server: %d\n", r)
		os.Exit(1)
	}

	var addr net.SockaddrIn
	r = libuv.Ip4Addr(c.AllocaCStr(host), c.Atoi(c.AllocaCStr(port)), &addr)
	if r != 0 {
		fmt.Fprintf(os.Stderr, "Failed to parse address: %d\n", r)
		os.Exit(1)
	}

	r = server.Bind((*net.SockAddr)(unsafe.Pointer(&addr)), 0)
	if r != 0 {
		fmt.Fprintf(os.Stderr, "Bind error %s\n", libuv.Strerror(libuv.Errno(r)))
		os.Exit(1)
	}

	// Set SO_REUSEADDR
	yes := c.Int(1)
	r = net.SetSockOpt(server.GetIoWatcherFd(), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, unsafe.Pointer(&yes), c.Uint(unsafe.Sizeof(yes)))
	if r != 0 {
		fmt.Fprintf(os.Stderr, "setsockopt error %s\n", libuv.Strerror(libuv.Errno(r)))
		os.Exit(1)
	}

	r = (*libuv.Stream)(&server).Listen(syscall.SOMAXCONN, onNewConnection)
	if r != 0 {
		fmt.Fprintf(os.Stderr, "Listen error %s\n", libuv.Strerror(libuv.Errno(r)))
		os.Exit(1)
	}

	r = libuv.SignalInit(loop, &sigintHandle)
	if r != 0 {
		fmt.Fprintf(os.Stderr, "Failed to initialize signal handler: %d\n", r)
		os.Exit(1)
	}
	r = sigintHandle.Start(onSignal, c.Int(syscall.SIGINT))
	if r != 0 {
		fmt.Fprintf(os.Stderr, "Failed to start signal handler: %d\n", r)
		os.Exit(1)
	}

	r = libuv.SignalInit(loop, &sigtermHandle)
	if r != 0 {
		fmt.Fprintf(os.Stderr, "Failed to initialize signal handler: %d\n", r)
		os.Exit(1)
	}
	r = sigtermHandle.Start(onSignal, c.Int(syscall.SIGTERM))
	if r != 0 {
		fmt.Fprintf(os.Stderr, "Failed to start signal handler: %d\n", r)
		os.Exit(1)
	}

	r = libuv.InitCheck(loop, &checkHandle)
	if r != 0 {
		fmt.Fprintf(os.Stderr, "Failed to initialize check handler: %d\n", r)
		os.Exit(1)
	}

	r = checkHandle.Start(onCheck)
	if r != 0 {
		fmt.Fprintf(os.Stderr, "Failed to start check handler: %d\n", r)
		os.Exit(1)
	}

	fmt.Printf("http handshake (hyper v%s) ...\n", c.GoString(hyper.Version()))

	r = loop.Run(libuv.RUN_DEFAULT)
	if r != 0 {
		fmt.Fprintf(os.Stderr, "Error in event loop: %v\n", r)
		os.Exit(1)
	}

	// Cleanup
	fmt.Println("Closing all handles...")
	loop.Walk(closeWalkCb, nil)
	loop.Run(libuv.RUN_DEFAULT)

	loop.Close()
    if exec != nil {
        exec.Free()
    }
    if http1Opts != nil {
        http1Opts.Free()
    }
    if http2Opts != nil {
        http2Opts.Free()
    }

	fmt.Println("Shutdown complete.")
	os.Exit(0)
}