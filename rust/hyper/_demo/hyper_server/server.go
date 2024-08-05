package main

import (
	"fmt"
	sysos "os"
	"unsafe"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/libuv"
	"github.com/goplus/llgo/c/net"
	"github.com/goplus/llgo/c/os"
	"github.com/goplus/llgo/c/syscall"
	"github.com/goplus/llgoexamples/rust/hyper"
)

const (
	MAX_EVENTS = 128
)

var (
	exec                        *hyper.Executor
	loop                        *libuv.Loop
	server                      libuv.Tcp
	sigintHandle, sigtermHandle libuv.Signal
	shouldExit                  = false
)

type ConnData struct {
	Stream       libuv.Tcp
	PollHandle   libuv.Poll
	EventMask    c.Uint
	ReadWaker    *hyper.Waker
	WriteWaker   *hyper.Waker
	ConnTask     *hyper.Task
	IsClosing    c.Int
	RequestCount c.Int
}

type ServiceUserdata struct {
	Host     [128]c.Char
	Port     [8]c.Char
	Executor *hyper.Executor
	Conn     *ConnData
}

func onSignal(handle *libuv.Signal, signum c.Int) {
	//fmt.Printf("Caught signal %d... exiting\n", signum)
	c.Printf(c.Str("Caught signal %d... exiting\n"), signum)
	shouldExit = true
	sigintHandle.Stop()
	sigtermHandle.Stop()
	(*libuv.Handle)(unsafe.Pointer(handle)).Close(nil)
	loop.Close()
}

func closeWalkCb(handle *libuv.Handle, arg c.Pointer) {
	// if handle.IsClosing() == 0 {
	// 	handle.Close(nil)
	// }
	handle.Close(nil)
}

func allocBuffer(handle *libuv.Handle, suggestedSize uintptr, buf *libuv.Buf) {
	buf.Base = (*c.Char)(c.Malloc(suggestedSize))
	buf.Len = suggestedSize
}

func onClose(handle *libuv.Handle) {
	c.Free(unsafe.Pointer(handle))
}

func closeConn(handle *libuv.Handle) {
	conn := (*ConnData)(handle.GetData())
	if conn != nil {
		fmt.Printf("Closing connection after handling %d requests\n", conn.RequestCount)
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
		c.Free(unsafe.Pointer(conn))
	}
	c.Free(unsafe.Pointer(handle))
}

func onPoll(handle *libuv.Poll, status c.Int, events c.Int) {
	conn := (*ConnData)(unsafe.Pointer((*libuv.Handle)(unsafe.Pointer(handle)).GetData()))

	if status < 0 {
		//fmt.Fprintf(os.Stderr, "Poll error: %s\n", libuv.Strerror(libuv.Errno(status)))
		c.Printf(c.Str("Poll error: %s\n"), libuv.Strerror(libuv.Errno(status)))
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
		//fmt.Fprintf(os.Stderr, "uv_poll_start error: %s\n", libuv.Strerror(libuv.Errno(r)))
		c.Printf(c.Str("uv_poll_start error: %s\n"), libuv.Strerror(libuv.Errno(r)))
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

	if syscall.Errno(ret) != syscall.EAGAIN && syscall.Errno(ret) != syscall.EWOULDBLOCK {
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

	if syscall.Errno(ret) != syscall.EAGAIN && syscall.Errno(ret) != syscall.EWOULDBLOCK {
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

func createConnData(client *libuv.Tcp) *ConnData {
	conn := (*ConnData)(c.Calloc(1, unsafe.Sizeof(ConnData{})))
	if conn == nil {
		//fmt.Fprintf(os.Stderr, "Failed to allocate conn_data\n")
		c.Printf(c.Str("Failed to malloc conn_data mem\n"))
		return nil
	}
	c.Memcpy(unsafe.Pointer(&conn.Stream), unsafe.Pointer(client), unsafe.Sizeof(libuv.Tcp{}))
	//c.Memmove(unsafe.Pointer(&conn.Stream), unsafe.Pointer(client), unsafe.Sizeof(libuv.Tcp{}))
	conn.IsClosing = 0
	conn.RequestCount = 0

	r := libuv.PollInit(loop, &conn.PollHandle, libuv.OsFd(client.GetIoWatcherFd()))
	if r < 0 {
		//fmt.Fprintf(os.Stderr, "uv_poll_init error: %s\n", libuv.Strerror(libuv.Errno(r)))
		c.Printf(c.Str("uv_poll_init error: %s\n"), libuv.Strerror(libuv.Errno(r)))
		c.Free(unsafe.Pointer(conn))
		return nil
	}

	(*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).SetData(unsafe.Pointer(conn))
	(*libuv.Handle)(unsafe.Pointer(&conn.Stream)).SetData(unsafe.Pointer(conn))

	if !updateConnDataRegistrations(conn, true) {
		(*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).Close(nil)
		c.Free(unsafe.Pointer(conn))
		return nil
	}

	return conn
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
	userdata := (*ServiceUserdata)(c.Calloc(1, unsafe.Sizeof(ServiceUserdata{})))
	if userdata == nil {
		//fmt.Fprintf(os.Stderr, "Failed to allocate service_userdata\n")
		c.Printf(c.Str("Failed to allocate service_userdata\n"))
	}
	return userdata
}

func freeServiceUserdata(userdata c.Pointer) {
	castUserdata := (*ServiceUserdata)(userdata)
	if castUserdata != nil {
		// Note: We don't free conn here because it's managed separately
		c.Free(userdata)
	}
}

func printEachHeader(userdata c.Pointer, name *byte, nameLen uintptr, value *byte, valueLen uintptr) c.Int {
	// fmt.Printf("%.*s: %.*s\n", int(nameLen), c.GoString((*c.Char)(c.Pointer(name))),
	// 	int(valueLen), c.GoString((*c.Char)(unsafe.Pointer(value))))
	c.Printf(c.Str("%.*s: %.*s\n"), nameLen, (*c.Char)(unsafe.Pointer(name)), valueLen, (*c.Char)(unsafe.Pointer(value)))
	return hyper.IterContinue
}

func printBodyChunk(userdata c.Pointer, chunk *hyper.Buf) c.Int {
	buf := chunk.Bytes()
	len := chunk.Len()
	os.Write(1, unsafe.Pointer(buf), len)

	return hyper.IterContinue
}

func sendEachBodyChunk(userdata c.Pointer, ctx *hyper.Context, chunk **hyper.Buf) c.Int {
	chunkCount := (*c.Int)(userdata)
	if *chunkCount > 0 {
		c.Printf(c.Str("Chunk %d\n"), *chunkCount)
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
		//fmt.Fprintf(os.Stderr, "Error: No connection data available\n")
		c.Printf(c.Str("Error: No connection data available\n"))
		return
	}

	conn.RequestCount++
	// fmt.Printf("Handling request %d on connection from %s:%s\n", conn.RequestCount,
	// 	c.GoString((*c.Char)(&serviceData.Host[0])), c.GoString((*c.Char)(&serviceData.Port[0])))
	c.Printf(c.Str("Handling request %d on connection from %s:%s\n"), c.Int(conn.RequestCount),
		(*c.Char)(&serviceData.Host[0]), (*c.Char)(&serviceData.Port[0]))

	// fmt.Printf("Received request from %s:%s\n", c.GoString((*c.Char)(&serviceData.Host[0])),
	// 	c.GoString((*c.Char)(&serviceData.Port[0])))
	c.Printf(c.Str("Received request from %s:%s\n"), (*c.Char)(&serviceData.Host[0]), (*c.Char)(&serviceData.Port[0]))

	if request == nil {
		//fmt.Fprintf(os.Stderr, "Error: Received null request\n")
		c.Printf(c.Str("Error: Received null request\n"))
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
		//fmt.Printf("Scheme: %s\n", string(scheme[:schemeLen]))
		c.Printf(c.Str("Scheme: %.*s\n"), c.Int(schemeLen), (*c.Char)(unsafe.Pointer(&scheme[0])))
		//fmt.Printf("Authority: %s\n", string(authority[:authorityLen]))
		c.Printf(c.Str("Authority: %.*s\n"), c.Int(authorityLen), (*c.Char)(unsafe.Pointer(&authority[0])))
		//fmt.Printf("Path and Query: %s\n", string(pathAndQuery[:pathAndQueryLen]))
		c.Printf(c.Str("Path and Query: %.*s\n"), c.Int(pathAndQueryLen), (*c.Char)(unsafe.Pointer(&pathAndQuery[0])))
	} else {
		//fmt.Fprintf(os.Stderr, "Failed to get URI parts. Error code: %d\n", uriResult)
		c.Printf(c.Str("Failed to get URI parts. Error code: %d\n"), uriResult)
	}

	version := request.Version()
	//fmt.Printf("HTTP Version: ")
	c.Printf(c.Str("HTTP Version: "))
	switch version {
	case hyper.HTTPVersionNone:
		//fmt.Println("None")
		c.Printf(c.Str("None\n"))
	case hyper.HTTPVersion10:
		//fmt.Println("HTTP/1.0")
		c.Printf(c.Str("HTTP/1.0\n"))
	case hyper.HTTPVersion11:
		//fmt.Println("HTTP/1.1")
		c.Printf(c.Str("HTTP/1.1\n"))
	case hyper.HTTPVersion2:
		//fmt.Println("HTTP/2")
		c.Printf(c.Str("HTTP/2\n"))
	default:
		//fmt.Printf("Unknown (%d)\n", version)
		c.Printf(c.Str("Unknown (%d)\n"), version)
	}

	var method [32]byte
	methodLen := unsafe.Sizeof(method)
	methodResult := request.Method(&method[0], &methodLen)
	if methodResult == hyper.OK {
		//fmt.Printf("Method: %s\n", string(method[:methodLen]))
		c.Printf(c.Str("Method: %.*s\n"), c.Int(methodLen), (*c.Char)(unsafe.Pointer(&method[0])))
	} else {
		//fmt.Fprintf(os.Stderr, "Failed to get request method. Error code: %d\n", methodResult)
		c.Printf(c.Str("Failed to get request method. Error code: %d\n"), methodResult)
	}

	//fmt.Println("Headers:")
	c.Printf(c.Str("Headers:\n"))
	reqHeaders := request.Headers()
	if reqHeaders != nil {
		reqHeaders.Foreach(printEachHeader, nil)
	} else {
		//fmt.Fprintf(os.Stderr, "Error: Failed to get request headers\n")
		c.Printf(c.Str("Error: Failed to get request headers\n"))
	}

	if methodLen > 0 && (c.Strncmp((*c.Char)(unsafe.Pointer(&method[0])), c.Str("POST"), methodLen) == 0 ||
		c.Strncmp((*c.Char)(unsafe.Pointer(&method[0])), c.Str("PUT"), methodLen) == 0) {
		//fmt.Println("Request Body:")
		c.Printf(c.Str("Request Body:\n"))
		body := request.Body()
		if body != nil {
			task := body.Foreach(printBodyChunk, nil, nil)
			if task != nil {
				r := serviceData.Executor.Push(task)
				if r != hyper.OK {
					//fmt.Fprintf(os.Stderr, "Error: Failed to push body foreach task\n")
					c.Printf(c.Str("Error: Failed to push body foreach task: %d\n"), r)
					task.Free()
				}
			} else {
				//fmt.Fprintf(os.Stderr, "Error: Failed to create body foreach task\n")
				c.Printf(c.Str("Error: Failed to create body foreach task\n"))
			}
		} else {
			//fmt.Fprintf(os.Stderr, "Error: Failed to get request body\n")
			c.Printf(c.Str("Error: Failed to get request body\n"))
		}
	}

	response := hyper.NewResponse()
	// if response != nil {
	// 	response.SetStatus(200)
	// 	rspHeaders := response.Headers()
	// 	if rspHeaders != nil {
	// 		rspHeaders.Set((*byte)(unsafe.Pointer(c.Str("Content-Type"))), uintptr(12), (*byte)(unsafe.Pointer(c.Str("text/plain"))), uintptr(10))
	// 		rspHeaders.Set((*byte)(unsafe.Pointer(c.Str("Cache-Control"))), uintptr(13), (*byte)(unsafe.Pointer(c.Str("no-cache"))), uintptr(8))
	// 	} else {
	// 		//fmt.Fprintf(os.Stderr, "Error: Failed to get response headers\n")
	// 		c.Printf(c.Str("Error: Failed to get response headers\n"))
	// 	}

	// 	if methodLen > 0 && c.Strncmp((*c.Char)(unsafe.Pointer(&method[0])), c.Str("GET"), methodLen) == 0 {
	// 		c.Printf(c.Str("Sending GET response\n"))
	// 		body := hyper.NewBody()
	// 		if body != nil {
	// 			body.SetDataFunc(sendEachBodyChunk)
	// 			chunkCount := (*c.Int)(c.Malloc(unsafe.Sizeof(c.Int(0))))
	// 			if chunkCount != nil {
	// 				*chunkCount = 10
	// 				body.SetUserdata(unsafe.Pointer(chunkCount), func(p c.Pointer) { c.Free(p) })
	// 				response.SetBody(body)
	// 			} else {
	// 				//fmt.Fprintf(os.Stderr, "Error: Failed to allocate chunk_count\n")
	// 				c.Printf(c.Str("Error: Failed to allocate chunk_count\n"))
	// 			}
	// 		} else {
	// 			//fmt.Fprintf(os.Stderr, "Error: Failed to create response body\n")
	// 			c.Printf(c.Str("Error: Failed to create response body\n"))
	// 		}
	// 	}

	// 	channel.Send(response)
	// } else {
	// 	//fmt.Fprintf(os.Stderr, "Error: Failed to create response\n")
	// 	c.Printf(c.Str("Error: Failed to create response\n"))
	// }
	if response != nil {
		response.SetStatus(200)
		channel.Send(response)
	} else {
		c.Printf(c.Str("Error: Failed to create response\n"))
	}

	// We don't close the connection here. Let hyper handle keep-alive.
}

func onNewConnection(serverStream *libuv.Stream, status c.Int) {
	if status < 0 {
		//fmt.Fprintf(os.Stderr, "New connection error %s\n", libuv.Strerror(libuv.Errno(status)))
		c.Printf(c.Str("New connection error %s\n"), libuv.Strerror(libuv.Errno(status)))
		return
	}

	client := (*libuv.Tcp)(c.Malloc(unsafe.Sizeof(libuv.Tcp{})))
	libuv.InitTcp(loop, client)

	if serverStream.Accept((*libuv.Stream)(unsafe.Pointer(client))) == 0 {
		userdata := createServiceUserdata()
		if userdata == nil {
			//fmt.Fprintf(os.Stderr, "Failed to create service_userdata\n")
			c.Printf(c.Str("Failed to create service_userdata\n"))
			(*libuv.Handle)(unsafe.Pointer(client)).Close(onClose)
			return
		}
		userdata.Executor = exec

		var addr net.SockaddrStorage
		addrlen := c.Int(unsafe.Sizeof(addr))
		client.Getpeername((*net.SockAddr)(c.Pointer(&addr)), &addrlen)

		if addr.Family == net.AF_INET {
			s := (*net.SockaddrIn)(unsafe.Pointer(&addr))
			libuv.Ip4Name(s, (*c.Char)(&userdata.Host[0]), unsafe.Sizeof(userdata.Host))
			c.Snprintf((*c.Char)(&userdata.Port[0]), unsafe.Sizeof(userdata.Port), c.Str("%d"), Ntohs(s.Port))
		} else if addr.Family == net.AF_INET6 {
			s := (*net.SockaddrIn6)(unsafe.Pointer(&addr))
			libuv.Ip6Name(s, (*c.Char)(&userdata.Host[0]), unsafe.Sizeof(userdata.Host))
			c.Snprintf((*c.Char)(&userdata.Port[0]), unsafe.Sizeof(userdata.Port), c.Str("%d"), Ntohs(s.Port))
		}

		// fmt.Printf("New incoming connection from (%s:%s)\n", c.GoString((*c.Char)(&userdata.Host[0])),
		// 	c.GoString((*c.Char)(&userdata.Port[0])))
		c.Printf(c.Str("New incoming connection from (%s:%s)\n"), (*c.Char)(&userdata.Host[0]),
			(*c.Char)(&userdata.Port[0]))

		conn := createConnData(client)
		if conn == nil {
			//fmt.Fprintf(os.Stderr, "Failed to create conn_data\n")
			c.Printf(c.Str("Failed to create conn_data\n"))
			(*libuv.Handle)(unsafe.Pointer(client)).Close(onClose)
			freeServiceUserdata(unsafe.Pointer(userdata))
			return
		}

		userdata.Conn = conn

		io := createIo(conn)

		service := hyper.ServiceNew(serverCallback)
		service.SetUserdata(unsafe.Pointer(userdata), freeServiceUserdata)

		http1Opts := hyper.Http1ServerconnOptionsNew(userdata.Executor)
		http1Opts.HeaderReadTimeout(1000 * 5)

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
		(*libuv.Handle)(unsafe.Pointer(client)).Close(onClose)
	}
}

func main() {
	exec = hyper.NewExecutor()
	if exec == nil {
		//fmt.Fprintf(os.Stderr, "Failed to create hyper executor\n")
		c.Printf(c.Str("Failed to create hyper executor\n"))
		os.Exit(1)
	}

	host := "127.0.0.1"
	port := "1234"
	if len(sysos.Args) > 1 {
		host = sysos.Args[1]
	}
	if len(sysos.Args) > 2 {
		port = sysos.Args[2]
	}
	//fmt.Printf("listening on port %s on %s...\n", port, host)
	c.Printf(c.Str("listening on port %s on %s...\n"), c.AllocaCStr(port), c.AllocaCStr(host))

	loop = libuv.DefaultLoop()

	libuv.InitTcp(loop, &server)

	var addr net.SockaddrIn
	libuv.Ip4Addr(c.AllocaCStr(host), c.Atoi(c.AllocaCStr(port)), &addr)

	r := server.Bind((*net.SockAddr)(unsafe.Pointer(&addr)), 0)
	if r != 0 {
		//fmt.Fprintf(os.Stderr, "Bind error %s\n", libuv.Strerror(libuv.Errno(r)))
		c.Printf(c.Str("Bind error %s\n"), libuv.Strerror(libuv.Errno(r)))
		os.Exit(1)
	}

	// Set SO_REUSEADDR
	yes := c.Int(1)
	r = net.SetSockOpt(server.GetIoWatcherFd(), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, unsafe.Pointer(&yes), c.Uint(unsafe.Sizeof(yes)))
	if r != 0 {
		//fmt.Fprintf(os.Stderr, "setsockopt error %s\n", libuv.Strerror(libuv.Errno(r)))
		c.Printf(c.Str("setsockopt error %s\n"), libuv.Strerror(libuv.Errno(r)))
		os.Exit(1)
	}

	r = (*libuv.Stream)(&server).Listen(syscall.SOMAXCONN, onNewConnection)
	if r != 0 {
		//fmt.Fprintf(os.Stderr, "Listen error %s\n", libuv.Strerror(libuv.Errno(r)))
		c.Printf(c.Str("Listen error %s\n"), libuv.Strerror(libuv.Errno(r)))
		os.Exit(1)
	}

	libuv.SignalInit(loop, &sigintHandle)
	sigintHandle.Start(onSignal, c.Int(syscall.SIGINT))

	libuv.SignalInit(loop, &sigtermHandle)
	sigtermHandle.Start(onSignal, c.Int(syscall.SIGTERM))

	//fmt.Printf("http handshake (hyper v%s) ...\n", c.GoString(hyper.Version()))
	c.Printf(c.Str("http handshake (hyper v%s) ...\n"), hyper.Version())
	for {
		loop.Run(libuv.RUN_NOWAIT)

		task := exec.Poll()
		for task != nil && !shouldExit {
			taskType := task.Type()
			taskUserdata := task.Userdata()

			switch taskType {
			case hyper.TaskEmpty:
				//fmt.Printf("\nEmpty task received: connection closed\n")
				c.Printf(c.Str("\nEmpty task received: connection closed\n"))
				if taskUserdata != nil {
					conn := (*ConnData)(taskUserdata)
					//fmt.Printf("Connection task completed for request %d\n", conn.RequestCount)
					c.Printf(c.Str("Connection task completed for request %d\n"), conn.RequestCount)
					c.Printf(c.Str("IsClosing: %d\n"), c.Int(conn.IsClosing))
					// if conn.IsClosing == 0 {
					// 	c.Printf(c.Str("Closing connection\n"))
					// 	conn.IsClosing = 1
					// 	if (*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).IsClosing() != 0 {
					// 		c.Printf(c.Str("Closing poll handle\n"))
					// 		(*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).Close(onClose)
					// 	}
					// 	if (*libuv.Handle)(unsafe.Pointer(&conn.Stream)).IsClosing() != 0 {
					// 		c.Printf(c.Str("Closing stream\n"))
					// 		(*libuv.Handle)(unsafe.Pointer(&conn.Stream)).Close(onClose)
					// 	}
					// }
					if (*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).IsClosing() == 0 {
						c.Printf(c.Str("Closing poll handle\n"))
						(*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).Close(onClose)
					}
					if (*libuv.Handle)(unsafe.Pointer(&conn.Stream)).IsClosing() == 0 {
						c.Printf(c.Str("Closing stream\n"))
						(*libuv.Handle)(unsafe.Pointer(&conn.Stream)).Close(onClose)
					}
				}
				conn := (*ConnData)(taskUserdata)
				if (*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).IsClosing() == 0 {
					c.Printf(c.Str("Closing poll handle\n"))
					(*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).Close(onClose)
				}
				if (*libuv.Handle)(unsafe.Pointer(&conn.Stream)).IsClosing() == 0 {
					c.Printf(c.Str("Closing stream\n"))
					(*libuv.Handle)(unsafe.Pointer(&conn.Stream)).Close(onClose)
				}
				break

			case hyper.TaskError:
				err := (*hyper.Error)(task.Value())
				var errbuf [256]byte
				errlen := err.Print(&errbuf[0], unsafe.Sizeof(errbuf))
				//fmt.Fprintf(os.Stderr, "Task error: %.*s\n", int(errlen), c.GoString((*c.Char)(unsafe.Pointer(&errbuf[0]))))
				c.Printf(c.Str("Task error: %.*s\n"), errlen, (*c.Char)(unsafe.Pointer(&errbuf[0])))
				err.Free()
				conn := (*ConnData)(taskUserdata)
				c.Printf(c.Str("IsClosing: %d\n"), c.Int(conn.IsClosing))
				// if conn.IsClosing == 0 {
				// 	c.Printf(c.Str("Closing connection\n"))
				// 	conn.IsClosing = 1
				// 	if (*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).IsClosing() != 0 {
				// 		c.Printf(c.Str("Closing poll handle\n"))
				// 		(*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).Close(onClose)
				// 	}
				// 	if (*libuv.Handle)(unsafe.Pointer(&conn.Stream)).IsClosing() != 0 {
				// 		c.Printf(c.Str("Closing stream\n"))
				// 		(*libuv.Handle)(unsafe.Pointer(&conn.Stream)).Close(onClose)
				// 	}
				// }
				if (*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).IsClosing() == 0 {
					c.Printf(c.Str("Closing poll handle\n"))
					(*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).Close(onClose)
				}
				if (*libuv.Handle)(unsafe.Pointer(&conn.Stream)).IsClosing() == 0 {
					c.Printf(c.Str("Closing stream\n"))
					(*libuv.Handle)(unsafe.Pointer(&conn.Stream)).Close(onClose)
				}
				break

			case hyper.TaskClientConn:
				//fmt.Fprintf(os.Stderr, "Unexpected HYPER_TASK_CLIENTCONN in server context\n")
				c.Printf(c.Str("Unexpected HYPER_TASK_CLIENTCONN in server context\n"))
				break

			case hyper.TaskResponse:
				//fmt.Println("Response task received")
				c.Printf(c.Str("Response task received\n"))
				break

			case hyper.TaskBuf:
				//fmt.Println("Buffer task received")
				c.Printf(c.Str("Buffer task received\n"))
				break

			case hyper.TaskServerconn:
				//fmt.Println("Server connection task received: ready for new connection...")
				c.Printf(c.Str("Server connection task received: ready for new connection...\n"))
				break

			default:
				//fmt.Fprintf(os.Stderr, "Unknown task type: %d\n", taskType)
				c.Printf(c.Str("Unknown task type: %d\n"), taskType)
				break
			}

			if taskUserdata == nil && taskType != hyper.TaskEmpty && taskType != hyper.TaskServerconn {
				//fmt.Fprintf(os.Stderr, "Warning: Task with no associated connection data. Type: %d\n", taskType)
				c.Printf(c.Str("Warning: Task with no associated connection data. Type: %d\n"), taskType)
			}

			task.Free()
			if !shouldExit {
				task = exec.Poll()
			}
		}

		if shouldExit {
			//fmt.Println("Shutdown initiated, cleaning up...")
			c.Printf(c.Str("Shutdown initiated, cleaning up...\n"))
			break
		}

		// Handle any pending closures
		loop.Run(libuv.RUN_NOWAIT)
	}

	// Cleanup
	//fmt.Println("Closing all handles...")
	c.Printf(c.Str("Closing all handles...\n"))
	loop.Walk(closeWalkCb, nil)

	loop.Run(libuv.RUN_DEFAULT)

	loop.Close()
	exec.Free()

	//fmt.Println("Shutdown complete.")
	c.Printf(c.Str("Shutdown complete.\n"))
}

// Ntohs converts a 16-bit integer from network byte order to host byte order.
func Ntohs(x uint16) uint16 {
	if isLittleEndian() {
		return ((x & 0xFF00) >> 8) | ((x & 0x00FF) << 8)
	}
	return x
}

// isLittleEndian checks if the host machine is little-endian.
func isLittleEndian() bool {
	var i int32 = 0x01020304
	return *(*byte)(unsafe.Pointer(&i)) == 0x04
}
