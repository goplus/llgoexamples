package main

import (
	"fmt"
	"os"
	"unsafe"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/libuv"
	"github.com/goplus/llgo/c/net"
	cos "github.com/goplus/llgo/c/os"
	"github.com/goplus/llgo/c/syscall"
	"github.com/goplus/llgoexamples/rust/hyper"
)

const (
	MAX_EVENTS       = 128
	READ_BUFFER_SIZE = 65536
)

var (
	exec                        *hyper.Executor
	loop                        *libuv.Loop
	server                      libuv.Tcp
	sigintHandle, sigtermHandle libuv.Signal
	shouldExit                  = false
)

type ConnData struct {
	Stream           libuv.Tcp
	ReadWaker        *hyper.Waker
	WriteWaker       *hyper.Waker
	ConnTask         *hyper.Task
	IsClosing        int
	ReadBuffer       libuv.Buf
	ReadBufferFilled uintptr
	WriteBuffer      libuv.Buf
	WriteReq         libuv.Write
}

type ServiceUserdata struct {
	Host     [128]c.Char
	Port     [8]c.Char
	Executor *hyper.Executor
	Conn     *ConnData
}

func onSignal(handle *libuv.Signal, sigNum c.Int) {
	fmt.Printf("Caught signal %d... exiting\n", sigNum)
	shouldExit = true
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

func allocBuffer(handle *libuv.Handle, suggestedSize uintptr, buf *libuv.Buf) {
	conn := (*ConnData)(handle.GetData())

	if conn.ReadBuffer.Base == nil {
		conn.ReadBuffer.Base = (*c.Char)(c.Malloc(suggestedSize))
		if conn.ReadBuffer.Base == nil {
			fmt.Fprintf(os.Stderr, "Failed to allocate read buffer\n")
			*buf = libuv.InitBuf(nil, 0)
			return
		}
		conn.ReadBuffer.Len = READ_BUFFER_SIZE
		conn.ReadBufferFilled = 0
	}

	available := READ_BUFFER_SIZE - conn.ReadBufferFilled
	toAlloc := available
	if uintptr(available) > suggestedSize {
		toAlloc = uintptr(suggestedSize)
	}

	*buf = libuv.InitBuf(
		(*c.Char)(unsafe.Pointer(uintptr(unsafe.Pointer(conn.ReadBuffer.Base))+uintptr(conn.ReadBufferFilled))),
		c.Uint(toAlloc),
	)
}

func onClose(handle *libuv.Handle) {
	c.Free(unsafe.Pointer(handle))
}

func closeConn(handle *libuv.Handle) {
	conn := (*ConnData)(handle.GetData())

	if conn != nil {
		if conn.ReadWaker != nil {
			conn.ReadWaker.Free()
			conn.ReadWaker = nil
		}
		if conn.WriteWaker != nil {
			conn.WriteWaker.Free()
			conn.WriteWaker = nil
		}
		if conn.ConnTask != nil {
			conn.ConnTask.Free()
			conn.ConnTask = nil
		}
	}
}

func onRead(client *libuv.Stream, nread c.Long, buf *libuv.Buf) {
	conn := (*ConnData)((*libuv.Handle)(unsafe.Pointer(client)).GetData())

	if nread < 0 {
		if libuv.Errno(nread) != libuv.EOF {
			fmt.Println("Read error")
			(*libuv.Handle)(unsafe.Pointer(client)).Close(closeConn)
			//c.Free(unsafe.Pointer(buf.Base))
		}
	} else if nread > 0 {
		conn.ReadBufferFilled += uintptr(nread)
	}

	if conn.ReadWaker != nil {
		conn.ReadWaker.Wake()
		conn.ReadWaker = nil
	}
}

func readCb(userdata unsafe.Pointer, ctx *hyper.Context, buf *byte, bufLen uintptr) uintptr {
	conn := (*ConnData)(userdata)

	// Copy data from the read buffer to the hyper context buffer
	if conn.ReadBufferFilled > 0 {
		toCopy := conn.ReadBufferFilled
		if uintptr(toCopy) > bufLen {
			toCopy = uintptr(bufLen)
		}
		c.Memcpy(unsafe.Pointer(buf), unsafe.Pointer(conn.ReadBuffer.Base), toCopy)

		c.Memmove(
			unsafe.Pointer(conn.ReadBuffer.Base),
			unsafe.Pointer(uintptr(unsafe.Pointer(conn.ReadBuffer.Base))+toCopy),
			uintptr(conn.ReadBufferFilled)-toCopy,
		)
		conn.ReadBufferFilled -= toCopy

		return toCopy
	}

	// Set the read waker
	if conn.ReadWaker != nil {
		conn.ReadWaker.Free()
	}

	conn.ReadWaker = ctx.Waker()
	return hyper.IoPending
}

func onWrite(req *libuv.Write, status c.Int) {
	conn := (*ConnData)(req.Data)

	// Check if the status is less than 0
	if status < 0 {
		fmt.Fprintf(os.Stderr, "Write completed with error: %s\n",
			libuv.Strerror(libuv.Errno(status)))
	}

	// Wake up the write waker
	if conn.WriteWaker != nil {
		conn.WriteWaker.Wake()
		conn.WriteWaker = nil
	}
}

func writeCb(userdata unsafe.Pointer, ctx *hyper.Context, buf *byte, bufLen uintptr) uintptr {
	conn := (*ConnData)(userdata)

	conn.WriteBuffer = libuv.InitBuf((*c.Char)(unsafe.Pointer(buf)), c.Uint(bufLen))
	conn.WriteReq.Data = unsafe.Pointer(conn)

	// Initiate the write operation
	r := conn.WriteReq.Write((*libuv.Stream)(unsafe.Pointer(&conn.Stream)), &conn.WriteBuffer, 1, onWrite)
	if r >= 0 {
		return bufLen
	}

	// Set the write_waker
	if conn.WriteWaker != nil {
		conn.WriteWaker.Free()
	}
	conn.WriteWaker = ctx.Waker()

	return hyper.IoPending
}

func createConnData() (*ConnData, error) {
	conn := &ConnData{}
	if conn == nil {
		return nil, fmt.Errorf("failed to allocate conn_data")
	}
	return conn, nil
}

func freeConnData(userdata c.Pointer) {
	conn := (*ConnData)(userdata)
	if conn != nil && conn.IsClosing == 0 {
		conn.IsClosing = 1
		// We don't immediately close the connection here.
		// Instead, we'll let the main loop handle the closure when appropriate.
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
	_ = buf
	_ = len

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

	conn := serviceData.Conn
	if conn == nil {
		fmt.Fprintf(os.Stderr, "Error: No connection data available\n")
		return
	}

	fmt.Printf("Handling request on connection from %s:%s\n", c.GoString((*c.Char)(&serviceData.Host[0])), c.GoString((*c.Char)(&serviceData.Port[0])))

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
}

func onNewConnection(server *libuv.Stream, status c.Int) {
	if status < 0 {
		fmt.Fprintf(os.Stderr, "New connection error %s\n", libuv.Strerror(libuv.Errno(status)))
		return
	}

	conn, err := createConnData()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create conn_data\n")
		(*libuv.Handle)(unsafe.Pointer(&conn.Stream)).Close(onClose)
		return
	}

	libuv.InitTcp(loop, &conn.Stream)
	if server.Accept((*libuv.Stream)(unsafe.Pointer(&conn.Stream))) == 0 {
		conn.Stream.Data = unsafe.Pointer(conn)

		r := (*libuv.Stream)(unsafe.Pointer(&conn.Stream)).StartRead(allocBuffer, onRead)
		if r < 0 {
			fmt.Fprintf(os.Stderr, "uv_read_start error: %s\n", libuv.Strerror(libuv.Errno(r)))
			c.Free(unsafe.Pointer(conn))
			return
		}

		userdata := createServiceUserdata()
		if userdata == nil {
			fmt.Fprintf(os.Stderr, "Failed to create service_userdata\n")
			(*libuv.Handle)(unsafe.Pointer(&conn.Stream)).Close(onClose)
			return
		}
		userdata.Executor = exec
		userdata.Conn = conn

		var addr net.SockaddrStorage
		addrlen := c.Int(unsafe.Sizeof(addr))
		conn.Stream.Getpeername((*net.SockAddr)(unsafe.Pointer(&addr)), &addrlen)

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

		http1Opts := hyper.Http1ServerconnOptionsNew(userdata.Executor)
		http1Opts.HeaderReadTimeout(5000)

		http2Opts := hyper.Http2ServerconnOptionsNew(userdata.Executor)
		http2Opts.KeepAliveInterval(5)
		http2Opts.KeepAliveTimeout(5)

		serverconn := hyper.ServeHttpXConnection(http1Opts, http2Opts, io, service)

		conn.ConnTask = serverconn
		serverconn.SetUserdata(unsafe.Pointer(conn), freeConnData)
		userdata.Executor.Push(serverconn)

		http1Opts.Free()
		http2Opts.Free()
	} else {
		(*libuv.Handle)(unsafe.Pointer(&conn.Stream)).Close(onClose)
	}
}

func main() {
	exec = hyper.NewExecutor()
	if exec == nil {
		fmt.Fprintf(os.Stderr, "Failed to create hyper executor\n")
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

	libuv.InitTcp(loop, &server)

	var addr net.SockaddrIn
	libuv.Ip4Addr(c.AllocaCStr(host), c.Atoi(c.AllocaCStr(port)), &addr)

	r := server.Bind((*net.SockAddr)(unsafe.Pointer(&addr)), 0)
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

	signalRes := libuv.SignalInit(loop, &sigintHandle)
	if signalRes != 0 {
		fmt.Fprintf(os.Stderr, "Failed to initialize signal handler: %d\n", signalRes)
		os.Exit(1)
	}
	signalHandleRes := sigintHandle.Start(onSignal, c.Int(syscall.SIGINT))
	if signalHandleRes != 0 {
		fmt.Fprintf(os.Stderr, "Failed to start signal handler: %d\n", signalHandleRes)
		os.Exit(1)
	}

	signalRes = libuv.SignalInit(loop, &sigtermHandle)
	if signalRes != 0 {
		fmt.Fprintf(os.Stderr, "Failed to initialize signal handler: %d\n", signalRes)
		os.Exit(1)
	}
	signalHandleRes = sigtermHandle.Start(onSignal, c.Int(syscall.SIGTERM))
	if signalHandleRes != 0 {
		fmt.Fprintf(os.Stderr, "Failed to start signal handler: %d\n", signalHandleRes)
		os.Exit(1)
	}

	fmt.Printf("http handshake (hyper v%s) ...\n", c.GoString(hyper.Version()))

	for {
		loop.Run(libuv.RUN_NOWAIT)

		task := exec.Poll()
		for task != nil && !shouldExit {
			taskType := task.Type()
			taskUserdata := task.Userdata()

			switch taskType {
			case hyper.TaskEmpty:
				fmt.Printf("\nEmpty task received: connection closed\n")
				if taskUserdata != nil {
					fmt.Println("taskUserdata is not nil")
					conn := (*ConnData)(taskUserdata)
					if conn.IsClosing == 0 {
						fmt.Println("conn.IsClosing is 0")
						conn.IsClosing = 1
						if (*libuv.Handle)(unsafe.Pointer(&conn.Stream)).IsClosing() == 0 {
							(*libuv.Handle)(unsafe.Pointer(&conn.Stream)).Close(closeConn)
						}
					}
				}

			case hyper.TaskError:
				fmt.Println("Task error:")
				err := (*hyper.Error)(task.Value())
				var errbuf [256]byte
				errlen := err.Print(&errbuf[0], unsafe.Sizeof(errbuf))
				fmt.Println("Error message:", string(errbuf[:errlen]))
				fmt.Printf("Error code: %d\n", int(err.Code()))
				err.Free()

			case hyper.TaskClientConn:
				fmt.Fprintf(os.Stderr, "Unexpected HYPER_TASK_CLIENTCONN in server context\n")

			case hyper.TaskResponse:
				fmt.Println("Response task received")

			case hyper.TaskBuf:
				fmt.Println("Buffer task received")

			case hyper.TaskServerconn:
				fmt.Println("Server connection task received: ready for new connection...")
			default:
				fmt.Fprintf(os.Stderr, "Unknown task type: %d\n", taskType)
			}

			if taskUserdata == nil && taskType != hyper.TaskEmpty && taskType != hyper.TaskServerconn {
				fmt.Fprintf(os.Stderr, "Warning: Task with no associated connection data. Type: %d\n", taskType)
			}

			if !shouldExit {
				task = exec.Poll()
			}

			task.Free()

		}

		if shouldExit {
			fmt.Println("Shutdown initiated, cleaning up...")
			break
		}

		// Handle any pending closures
		loop.Run(libuv.RUN_NOWAIT)
	}

	// Cleanup
	fmt.Println("Closing all handles...")
	loop.Walk(closeWalkCb, nil)
	loop.Run(libuv.RUN_DEFAULT)

	loop.Close()

	fmt.Println("Shutdown complete.")
	os.Exit(0)
}
