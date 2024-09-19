package http

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/libuv"
	cnet "github.com/goplus/llgo/c/net"
	cos "github.com/goplus/llgo/c/os"
	"github.com/goplus/llgo/c/syscall"
	"github.com/goplus/llgo/rust/hyper"
	"github.com/goplus/llgo/x/net"
)

// var requestNotifyHandle *libuv.Async
const _SC_NPROCESSORS_ONLN c.Int = 58

var cpuCount int
var asyncHandleMapMu sync.Mutex
var asyncHandleMap = make(map[int]*libuv.Async)
var connID int32

type Handler interface {
	ServeHTTP(ResponseWriter, *Request)
}

type ResponseWriter interface {
	Header() Header
	Write([]byte) (int, error)
	WriteHeader(statusCode int)
}

type Server struct {
	Addr    string
	Handler Handler

	// uvLoop   *libuv.Loop
	// uvServer libuv.Tcp

	isShutdown atomic.Bool
	// idleHandle libuv.Idle

	// executor  *hyper.Executor
	// http1Opts *hyper.Http1ServerconnOptions
	// http2Opts *hyper.Http2ServerconnOptions

	eventLoop []*eventLoop

	// mu                sync.Mutex
	// activeConnections map[*conn]struct{}
}

type eventLoop struct {
	uvLoop     *libuv.Loop
	uvServer   libuv.Tcp
	idleHandle libuv.Idle

	executor   *hyper.Executor
	http1Opts  *hyper.Http1ServerconnOptions
	http2Opts  *hyper.Http2ServerconnOptions
	isShutdown atomic.Bool

	mu                sync.Mutex
	activeConnections map[*conn]struct{}
}

type conn struct {
	asyncID       int
	stream        libuv.Tcp
	pollHandle    libuv.Poll
	eventMask     c.Uint
	readWaker     *hyper.Waker
	writeWaker    *hyper.Waker
	isClosing     atomic.Bool
	closedHandles int32
	remoteAddr    string
}

type serviceUserdata struct {
	asyncHandleID int
	host          [128]c.Char
	port          [8]c.Char
	executor      *hyper.Executor
}

type threadArg struct {
	host      string
	port      int
	eventLoop *eventLoop
}

func NewServer(addr string) *Server {
	return &Server{
		Addr:    addr,
		Handler: DefaultServeMux,
	}
}

func newEventLoop() (*eventLoop, error) {
	activeClients := make(map[*conn]struct{})
	el := &eventLoop{
		activeConnections: activeClients,
	}

	executor := hyper.NewExecutor()
	if executor == nil {
		return nil, fmt.Errorf("failed to create Executor")
	}
	el.executor = executor

	http1Opts := hyper.Http1ServerconnOptionsNew(el.executor)
	if http1Opts == nil {
		return nil, fmt.Errorf("failed to create http1_opts")
	}
	if hyperResult := http1Opts.HeaderReadTimeout(5 * 1000); hyperResult != hyper.OK {
		return nil, fmt.Errorf("failed to set header read timeout for http1_opts")
	}
	el.http1Opts = http1Opts

	http2Opts := hyper.Http2ServerconnOptionsNew(el.executor)
	if http2Opts == nil {
		return nil, fmt.Errorf("failed to create http2_opts")
	}
	if hyperResult := http2Opts.KeepAliveInterval(5); hyperResult != hyper.OK {
		return nil, fmt.Errorf("failed to set keep alive interval for http2_opts")
	}
	if hyperResult := http2Opts.KeepAliveTimeout(5); hyperResult != hyper.OK {
		return nil, fmt.Errorf("failed to set keep alive timeout for http2_opts")
	}
	el.http2Opts = http2Opts

	el.uvLoop = libuv.LoopNew()
	if el.uvLoop == nil {
		return nil, fmt.Errorf("failed to get default loop")
	}
	el.uvLoop.SetData(unsafe.Pointer(el))

	if r := libuv.InitTcpEx(el.uvLoop, &el.uvServer, cnet.AF_INET); r != 0 {
		return nil, fmt.Errorf("failed to init TCP: %v", libuv.Strerror(libuv.Errno(r)))
	}

	return el, nil
}

func (el *eventLoop) run(host string, port int) error {
	var sockaddr cnet.SockaddrIn
	if r := libuv.Ip4Addr(c.AllocaCStr(host), c.Int(port), &sockaddr); r != 0 {
		return fmt.Errorf("failed to create IP address: %v", libuv.Strerror(libuv.Errno(r)))
	}

	// Set SO_REUSEADDR
	// yes := c.Int(1)
	// fmt.Println("[debug] el.uvServer.GetIoWatcherFd(): ", el.uvServer.GetIoWatcherFd())
	// result := cnet.SetSockOpt(el.uvServer.GetIoWatcherFd(), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, unsafe.Pointer(&yes), c.Uint(unsafe.Sizeof(yes)))
	// if result != 0 {
	// 	return fmt.Errorf("failed to set SO_REUSEADDR: %v", result)
	// }

	// result = cnet.SetSockOpt(el.uvServer.GetIoWatcherFd(), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, unsafe.Pointer(&yes), c.Uint(unsafe.Sizeof(yes)))
	// if result != 0 {
	// 	return fmt.Errorf("failed to set SO_REUSEADDR: %v", result)
	// }

	if err := setReuseAddr(&el.uvServer); err != nil {
		return fmt.Errorf("failed to set SO_REUSEADDR: %v", err)
	}

	if r := el.uvServer.Bind((*cnet.SockAddr)(unsafe.Pointer(&sockaddr)), 0); r != 0 {
		return fmt.Errorf("failed to bind: %v", libuv.Strerror(libuv.Errno(r)))
	}

	//el.uvServer.Data = unsafe.Pointer(el)
	if err := (*libuv.Stream)(&el.uvServer).Listen(128, onNewConnection); err != 0 {
		return fmt.Errorf("failed to listen: %v", err)
	}

	if r := libuv.InitIdle(el.uvLoop, &el.idleHandle); r != 0 {
		return fmt.Errorf("failed to initialize idle handler: %d", r)
	}

	(*libuv.Handle)(unsafe.Pointer(&el.idleHandle)).SetData(unsafe.Pointer(el))

	if r := el.idleHandle.Start(onIdle); r != 0 {
		return fmt.Errorf("failed to start idle handler: %d", r)
	}

	//os.Setenv("UV_THREADPOOL_SIZE", "1")

	if r := el.uvLoop.Run(libuv.RUN_DEFAULT); r != 0 {
		return fmt.Errorf("error in event loop: %d", r)
	}

	return nil
}

func setReuseAddr(handle *libuv.Tcp) error {
	var fd libuv.OsFd
	result := (*libuv.Handle)(unsafe.Pointer(handle)).Fileno(&fd)
	if result != 0 {
		return fmt.Errorf("Error getting file descriptor")
	}

	yes := c.Int(1)
	if err := cnet.SetSockOpt(c.Int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, unsafe.Pointer(&yes), c.Uint(unsafe.Sizeof(yes))); err != 0 {
		return fmt.Errorf("Error setting SO_REUSEADDR")
	}

	if err := cnet.SetSockOpt(c.Int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, unsafe.Pointer(&yes), c.Uint(unsafe.Sizeof(yes))); err != 0 {
		return fmt.Errorf("Error setting SO_REUSEPORT")
	}

	return nil
}

// ErrServerClosed is returned by the [Server.Serve], [ServeTLS], [ListenAndServe],
// and [ListenAndServeTLS] methods after a call to [Server.Shutdown] or [Server.Close].
var ErrServerClosed = errors.New("http: Server closed")

func ListenAndServe(addr string, handler Handler) error {
	server := &Server{Addr: addr, Handler: handler}
	return server.ListenAndServe()
}

func (srv *Server) ListenAndServe() error {
	cpuCount = int(c.Sysconf(_SC_NPROCESSORS_ONLN))
	if cpuCount <= 0 {
		cpuCount = 4
	}

	fmt.Printf("[debug] cpuCount: %d\n", cpuCount)

	for i := 0; i < cpuCount; i++ {
		el, err := newEventLoop()
		if err != nil {
			return fmt.Errorf("failed to create event loop: %v", err)
		}
		srv.eventLoop = append(srv.eventLoop, el)
	}

	// el, err := newEventLoop()
	// if err != nil {
	// 	return fmt.Errorf("failed to create event loop: %v", err)
	// }
	// el2, err := newEventLoop()
	// if err != nil {
	// 	return fmt.Errorf("failed to create event loop: %v", err)
	// }

	host, port, err := net.SplitHostPort(srv.Addr)
	if err != nil {
		return fmt.Errorf("invalid address %q: %v", srv.Addr, err)
	}

	portNum, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("invalid port number: %v", err)
	}

	// go func() {
	// 	err = el2.run(host, portNum)
	// 	if err != nil {
	// 		println("[debug] failed to run event loop: %v", err)
	// 	}
	// }()

	//TODO(hackerchai): new logic for poll
	// go func() {
	// 	for {
	// 		task := el.executor.Poll()
	// 		if task != nil {
	// 			handleTask(task)
	// 			task = el.executor.Poll()
	// 		}
	// 	}
	// }()

	// err = el.run(host, portNum)
	// if err != nil {
	// 	return fmt.Errorf("failed to run event loop: %v", err)
	// }

	// Create a libuv thread pool with the same number of threads as event loops
	threadPool := make([]*libuv.Thread, len(srv.eventLoop))
	for i := range threadPool {
		threadPool[i] = &libuv.Thread{}
	}

	// Start each event loop in its own thread
	for i, el := range srv.eventLoop {
		threadArg := &threadArg{
			host:      host,
			port:      portNum,
			eventLoop: el,
		}

		fmt.Printf("[debug] Creating thread %d\n", i)

		if result := threadPool[i].Create(runEventLoopInThread, unsafe.Pointer(threadArg)); result != 0 {
			return fmt.Errorf("failed to create thread: %v", err)
		}
	}

	// Wait for all threads to complete
	for _, thread := range threadPool {
		if result := thread.Join(); result != 0 {
			fmt.Printf("[debug] Failed to join thread: %v\n", err)
		}
	}

	fmt.Printf("Listening on %s\n", srv.Addr)

	// if r := srv.uvServer.Bind((*cnet.SockAddr)(unsafe.Pointer(&sockaddr)), 0); r != 0 {
	// 	return fmt.Errorf("failed to bind: %v", libuv.Strerror(libuv.Errno(r)))
	// }

	return nil
}

func runEventLoopInThread(arg c.Pointer) {
	tArg := (*threadArg)(arg)
	host := tArg.host
	port := tArg.port
	el := tArg.eventLoop
	err := el.run(host, port)
	if err != nil {
		fmt.Printf("[debug] failed to run event loop: %v", err)
	}
}

func HandleFunc(pattern string, handler func(ResponseWriter, *Request)) {
	DefaultServeMux.HandleFunc(pattern, handler)
}

func onNewConnection(serverStream *libuv.Stream, status c.Int) {
	fmt.Println("[debug] onNewConnection called")
	if status < 0 {
		fmt.Printf("New connection error: %s\n", libuv.Strerror(libuv.Errno(status)))
		return
	}

	// srv := (*Server)((*libuv.Handle)(unsafe.Pointer(serverStream)).GetData())
	// if srv == nil {
	// 	fmt.Fprintf(os.Stderr, "Server is nil\n")
	// 	return
	// }
	// el := (*eventLoop)((*libuv.Handle)(unsafe.Pointer(serverStream)).GetData())
	// if el == nil {
	// 	fmt.Fprintf(os.Stderr, "Event loop is nil\n")
	// 	return
	// }
	el := (*eventLoop)((*libuv.Handle)(unsafe.Pointer(serverStream)).GetLoop().GetData())
	if el == nil {
		fmt.Fprintf(os.Stderr, "Event loop is nil\n")
		return
	}

	conn, err := createConnData()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create Conn: %v\n", err)
		return
	}

	fmt.Println("[debug] async handle creating")

	requestNotifyHandle := &libuv.Async{}
	el.uvLoop.Async(requestNotifyHandle, onAsync)
	fmt.Println("[debug] async handle created")
	asyncHandleMapMu.Lock()
	asyncHandleMap[conn.asyncID] = requestNotifyHandle
	asyncHandleMapMu.Unlock()
	fmt.Println("[debug] async handle added to map")

	libuv.InitTcp(el.uvLoop, &conn.stream)
	conn.stream.Data = unsafe.Pointer(conn)

	if serverStream.Accept((*libuv.Stream)(unsafe.Pointer(&conn.stream))) == 0 {
		fmt.Println("[debug] Accepted new connection")

		userData := createServiceUserdata()
		if userData == nil {
			fmt.Fprintf(os.Stderr, "Failed to create service userdata\n")
			(*libuv.Handle)(unsafe.Pointer(&conn.stream)).Close(nil)
			return
		}

		if el.executor == nil {
			fmt.Fprintf(os.Stderr, "Failed to get executor\n")
			(*libuv.Handle)(unsafe.Pointer(&conn.stream)).Close(nil)
			return
		}
		//userData.setExecutor(srv.executor)
		userData.executor = el.executor
		userData.asyncHandleID = conn.asyncID

		// if srv.Handler == nil {
		// 	fmt.Fprintf(os.Stderr, "Failed to get handler\n")
		// 	(*libuv.Handle)(unsafe.Pointer(&conn.stream)).Close(nil)
		// 	return
		// }
		//userData.handler = srv.Handler
		//userData.requestNotifyHandle = requestNotifyHandle

		var addr cnet.SockaddrStorage
		addrlen := c.Int(unsafe.Sizeof(addr))
		conn.stream.Getpeername((*cnet.SockAddr)(c.Pointer(&addr)), &addrlen)

		if addr.Family == cnet.AF_INET {
			s := (*cnet.SockaddrIn)(unsafe.Pointer(&addr))
			libuv.Ip4Name(s, (*c.Char)(&userData.host[0]), unsafe.Sizeof(userData.host))
			c.Snprintf((*c.Char)(&userData.port[0]), unsafe.Sizeof(userData.port), c.Str("%d"), cnet.Ntohs(s.Port))
		} else if addr.Family == cnet.AF_INET6 {
			s := (*cnet.SockaddrIn6)(unsafe.Pointer(&addr))
			libuv.Ip6Name(s, (*c.Char)(&userData.host[0]), unsafe.Sizeof(userData.host))
			c.Snprintf((*c.Char)(&userData.port[0]), unsafe.Sizeof(userData.port), c.Str("%d"), cnet.Ntohs(s.Port))
		}

		//TODO(hackerchai): use userData.host and userData.port
		conn.remoteAddr = c.GoString((*c.Char)(&userData.host[0])) + ":" + c.GoString((*c.Char)(&userData.port[0]))

		r := libuv.PollInit(el.uvLoop, &conn.pollHandle, libuv.OsFd(conn.stream.GetIoWatcherFd()))
		if r < 0 {
			fmt.Fprintf(os.Stderr, "uv_poll_init error: %s\n", libuv.Strerror(libuv.Errno(r)))
			(*libuv.Handle)(unsafe.Pointer(&conn.stream)).Close(nil)
			return
		}

		(*libuv.Handle)(unsafe.Pointer(&conn.pollHandle)).Data = unsafe.Pointer(conn)

		if !updateConnRegistrations(conn) {
			(*libuv.Handle)(unsafe.Pointer(&conn.pollHandle)).Close(nil)
			(*libuv.Handle)(unsafe.Pointer(&conn.stream)).Close(nil)
			return
		}

		fmt.Println("[debug] Conn created")
		el.trackConn(conn, true)
		fmt.Println("[debug] Conn tracked")

		io := createIo(conn)
		service := hyper.ServiceNew(serverCallback)
		service.SetUserdata(unsafe.Pointer(userData), nil)

		serverConn := hyper.ServeHttpXConnection(el.http1Opts, el.http2Opts, io, service)
		el.executor.Push(serverConn)
	} else {
		fmt.Println("[debug] Client not accepted")
		(*libuv.Handle)(unsafe.Pointer(&conn.pollHandle)).Close(nil)
		(*libuv.Handle)(unsafe.Pointer(&conn.stream)).Close(nil)
	}
}

func onAsync(asyncHandle *libuv.Async) {
	fmt.Println("[debug] onAsync called")
	taskData := (*taskData)(asyncHandle.GetData())
	if taskData == nil {
		fmt.Println("[debug] taskData is nil")
		return
	}
	dataTask := taskData.hyperBody.Data()
	dataTask.SetUserdata(c.Pointer(taskData), nil)
	if dataTask != nil {
		r := taskData.executor.Push(dataTask)
		fmt.Printf("[debug] onAsync push data task: %d\n", r)
		if r != hyper.OK {
			fmt.Printf("failed to push data task: %d\n", r)
			dataTask.Free()
		}
	}
}

func onIdle(handle *libuv.Idle) {
	// el := (*eventLoop)((*libuv.Handle)(unsafe.Pointer(handle)).GetData())
	el := (*eventLoop)((*libuv.Handle)(unsafe.Pointer(handle)).GetLoop().GetData())
	if el.executor != nil {
		task := el.executor.Poll()
		for task != nil {
			handleTask(task)
			task = el.executor.Poll()
		}
	}

	if el.shuttingDown() {
		fmt.Println("Shutdown initiated, cleaning up...")
		handle.Stop()
	}
}

func doNothing(handle *libuv.Idle) {
	return
}

// func (s *serviceUserdata) setExecutor(exec *hyper.Executor) {
// 	s.executor.Store(exec)
// }

// func (s *serviceUserdata) getExecutor() *hyper.Executor {
// 	return s.executor.Load()
// }

func serverCallback(userData unsafe.Pointer, hyperReq *hyper.Request, channel *hyper.ResponseChannel) {
	payload := (*serviceUserdata)(userData)
	// srv := userData.server
	// if srv == nil {
	// 	fmt.Fprintf(os.Stderr, "Error: Received null server\n")
	// 	return
	// }
	if payload == nil {
		fmt.Fprintf(os.Stderr, "Error: Received null userData\n")
		return
	}

	executor := payload.executor
	if executor == nil {
		fmt.Fprintf(os.Stderr, "Error: Received null executor\n")
		return
	}

	if hyperReq == nil {
		fmt.Fprintf(os.Stderr, "Error: Received null request\n")
		return
	}

	connID := payload.asyncHandleID
	asyncHandleMapMu.Lock()
	requestNotifyHandle, ok := asyncHandleMap[connID]
	asyncHandleMapMu.Unlock()
	if !ok {
		fmt.Println("[debug] requestNotifyHandle not found")
		return
	}

	host := payload.host
	port := payload.port
	remoteAddr := c.GoString(&host[0]) + ":" + c.GoString(&port[0])
	fmt.Printf("[debug] Remote address: %s\n", remoteAddr)

	req, err := readRequest(executor, hyperReq, requestNotifyHandle, remoteAddr)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		return
	}

	res := newResponse(channel)
	fmt.Println("[debug] Response created")

	//TODO(hackerchai): replace with no goroutine
	fmt.Println("[debug] Serving HTTP")
	DefaultServeMux.ServeHTTP(res, req)
	//srv.Handler.ServeHTTP(res, req)
	fmt.Println("[debug] Response finalizing")
	res.finalize()
	fmt.Println("[debug] Response finalized")

	// go func() {
	// 	fmt.Println("[debug] Serving HTTP")
	// 	DefaultServeMux.ServeHTTP(res, req)
	// 	//srv.Handler.ServeHTTP(res, req)
	// 	fmt.Println("[debug] Response finalizing")
	// 	res.finalize()
	// 	fmt.Println("[debug] Response finalized")
	// }()
}

func handleTask(task *hyper.Task) {
	hyperTaskType := task.Type()
	// Debug
	fmt.Printf("[debug] Task type: %s\n", getTaskTypeString(hyperTaskType))

	payload := (*taskData)(task.Userdata())

	// Debug
	if payload == nil {
		fmt.Println("[debug] task data is nil")
	}

	if payload != nil {
		switch payload.taskFlag {
		case getBodyTask:
			handleGetBodyTask(hyperTaskType, task, payload)
		case setBodyTask:
			handleSetBodyTask(hyperTaskType, task)
			return
		default:
			fmt.Println("[debug] Unknown response task type")
			return
		}
	}

	switch hyperTaskType {
	case hyper.TaskError:
		handleTaskError(task)
		return
	case hyper.TaskEmpty:
		fmt.Println("[debug] Empty task handled")
		task.Free()
		return
	case hyper.TaskServerconn:
		fmt.Println("[debug] Server connection task handled")
		task.Free()
		return
	default:
		fmt.Println("[debug] Unknown task type")
		return
	}
}

func handleGetBodyTask(hyperTaskType hyper.TaskReturnType, task *hyper.Task, payload *taskData) {
	switch hyperTaskType {
	case hyper.TaskError:
		handleTaskError(task)
	case hyper.TaskBuf:
		handleTaskBuffer(task, payload)
	case hyper.TaskEmpty:
		fmt.Println("[debug] Get body task closing request body")
		if payload.requestBody != nil {
			payload.requestBody.Close()
		}
		task.Free()
	}
}

func handleSetBodyTask(hyperTaskType hyper.TaskReturnType, task *hyper.Task) {
	switch hyperTaskType {
	case hyper.TaskError:
		handleTaskError(task)
	case hyper.TaskEmpty:
		fmt.Println("[debug] Set body task freeing")
		task.Free()
	}
}

func handleTaskError(task *hyper.Task) {
	err := (*hyper.Error)(task.Value())
	fmt.Printf("Error code: %d\n", err.Code())

	var errbuf [256]byte
	errlen := err.Print(&errbuf[0], unsafe.Sizeof(errbuf))
	fmt.Printf("Details: %s\n", errbuf[:errlen])
	err.Free()
	task.Free()
}

func handleTaskBuffer(task *hyper.Task, payload *taskData) {
	buf := (*hyper.Buf)(task.Value())
	bytes := unsafe.Slice(buf.Bytes(), buf.Len())
	payload.requestBody.readCh <- bytes
	fmt.Printf("[debug] Task get body writing to bodyWriter: %s\n", string(bytes))
	buf.Free()
	task.Free()
}

func getTaskTypeString(taskType hyper.TaskReturnType) string {
	switch taskType {
	case hyper.TaskEmpty:
		return "Empty"
	case hyper.TaskBuf:
		return "Buffer"
	case hyper.TaskError:
		return "Error"
	case hyper.TaskServerconn:
		return "Server connection"
	case hyper.TaskClientConn:
		return "Client connection"
	case hyper.TaskResponse:
		return "Response"
	default:
		return "Unknown"
	}
}

func (el *eventLoop) trackConn(c *conn, add bool) {
	el.mu.Lock()
	defer el.mu.Unlock()
	if el.activeConnections == nil {
		el.activeConnections = make(map[*conn]struct{})
	}
	if add {
		el.activeConnections[c] = struct{}{}
	} else {
		delete(el.activeConnections, c)
	}
}

func createIo(conn *conn) *hyper.Io {
	io := hyper.NewIo()
	io.SetUserdata(unsafe.Pointer(conn), freeConnData)
	io.SetRead(readCb)
	io.SetWrite(writeCb)
	return io
}

func createServiceUserdata() *serviceUserdata {
	userdata := &serviceUserdata{}
	if userdata == nil {
		fmt.Fprintf(os.Stderr, "Failed to allocate service_userdata\n")
	}
	return userdata
}

func readCb(userdata unsafe.Pointer, ctx *hyper.Context, buf *byte, bufLen uintptr) uintptr {
	conn := (*conn)(userdata)
	ret := cnet.Recv(conn.stream.GetIoWatcherFd(), unsafe.Pointer(buf), bufLen, 0)

	if ret >= 0 {
		return uintptr(ret)
	}

	if uintptr(cos.Errno) != syscall.EAGAIN && uintptr(cos.Errno) != syscall.EWOULDBLOCK {
		return hyper.IoError
	}

	if conn.readWaker != nil {
		conn.readWaker.Free()
	}

	if conn.eventMask&c.Uint(libuv.READABLE) == 0 {
		conn.eventMask |= c.Uint(libuv.READABLE)
		fmt.Printf("[debug] ReadCb Event mask: %d\n", conn.eventMask)
		if !updateConnRegistrations(conn) {
			return hyper.IoError
		}
		fmt.Printf("[debug] ReadCb updateConnRegistrations\n")
	}

	conn.readWaker = ctx.Waker()
	return hyper.IoPending
}

func writeCb(userdata unsafe.Pointer, ctx *hyper.Context, buf *byte, bufLen uintptr) uintptr {
	conn := (*conn)(userdata)
	ret := cnet.Send(conn.stream.GetIoWatcherFd(), unsafe.Pointer(buf), bufLen, 0)

	if ret >= 0 {
		return uintptr(ret)
	}

	if uintptr(cos.Errno) != syscall.EAGAIN && uintptr(cos.Errno) != syscall.EWOULDBLOCK {
		return hyper.IoError
	}

	if conn.writeWaker != nil {
		conn.writeWaker.Free()
	}

	if conn.eventMask&c.Uint(libuv.WRITABLE) == 0 {
		conn.eventMask |= c.Uint(libuv.WRITABLE)
		fmt.Printf("[debug] WriteCb Event mask: %d\n", conn.eventMask)
		if !updateConnRegistrations(conn) {
			return hyper.IoError
		}
	}

	conn.writeWaker = ctx.Waker()
	return hyper.IoPending
}

func onPoll(handle *libuv.Poll, status c.Int, events c.Int) {
	conn := (*conn)((*libuv.Handle)(unsafe.Pointer(handle)).GetData())

	if status < 0 {
		fmt.Fprintf(os.Stderr, "Poll error: %s\n", libuv.Strerror(libuv.Errno(status)))
		return
	}

	if events&c.Int(libuv.READABLE) != 0 && conn.readWaker != nil {
		conn.readWaker.Wake()
		conn.readWaker = nil
	}

	if events&c.Int(libuv.WRITABLE) != 0 && conn.writeWaker != nil {
		conn.writeWaker.Wake()
		conn.writeWaker = nil
	}
}

func updateConnRegistrations(conn *conn) bool {
	fmt.Println("[debug] updateConnRegistrations called")

	events := c.Int(0)
	if conn.eventMask == 0 {
		fmt.Println("[debug] No events to poll, skipping poll start.")
		return true
	}
	fmt.Printf("[debug] Event mask: %d\n", conn.eventMask)
	if conn.eventMask&c.Uint(libuv.READABLE) != 0 {
		events |= c.Int(libuv.READABLE)
	}
	if conn.eventMask&c.Uint(libuv.WRITABLE) != 0 {
		events |= c.Int(libuv.WRITABLE)
	}

	fmt.Printf("[debug] Starting poll with events: %d\n", events)
	r := conn.pollHandle.Start(events, onPoll)
	if r < 0 {
		fmt.Fprintf(os.Stderr, "uv_poll_start error: %s\n", libuv.Strerror(libuv.Errno(r)))
		return false
	}
	return true
}

func createConnData() (*conn, error) {
	conn := &conn{}
	if conn == nil {
		return nil, fmt.Errorf("failed to allocate conn_data")
	}
	conn.isClosing.Store(false)
	conn.closedHandles = 0
	conn.asyncID = int(connID) + 1

	return conn, nil
}

func freeConnData(userdata c.Pointer) {
	conn := (*conn)(userdata)
	conn.Close()
}

func closeWalkCb(handle *libuv.Handle, arg c.Pointer) {
	if handle.IsClosing() == 0 {
		handle.Close(nil)
	}
}

func (srv *Server) Close() error {
	srv.isShutdown.Store(true)

	// for c := range el.activeConnections {
	// 	c.Close()

	// 	delete(srv.activeConnections, c)
	// }

	// if srv.executor != nil {
	// 	srv.executor.Free()
	// 	srv.executor = nil
	// }

	// if exec := srv.executor; exec != nil {
	// 	srv.executor = nil
	// 	exec.Free()
	// }

	// if srv.http1Opts != nil {
	// 	srv.http1Opts.Free()
	// 	srv.http1Opts = nil
	// }

	// if srv.http2Opts != nil {
	// 	srv.http2Opts.Free()
	// 	srv.http2Opts = nil
	// }

	// srv.uvLoop.Walk(closeWalkCb, nil)
	// srv.uvLoop.Run(libuv.RUN_ONCE)
	// (*libuv.Handle)(unsafe.Pointer(&srv.uvServer)).Close(nil)

	// srv.uvLoop.Close()
	return nil
}

func (s *Server) shuttingDown() bool {
	return s.isShutdown.Load()
}

func (el *eventLoop) shuttingDown() bool {
	return el.isShutdown.Load()
}

func (c *conn) shuttingDown() bool {
	return c.isClosing.Load()
}

func (c *conn) Close() {
	if c != nil && !c.isClosing.Swap(true) {
		fmt.Printf("[debug] Closing connection...\n")
		if c.readWaker != nil {
			c.readWaker.Free()
			c.readWaker = nil
		}
		if c.writeWaker != nil {
			c.writeWaker.Free()
			c.writeWaker = nil
		}

		if (*libuv.Handle)(unsafe.Pointer(&c.pollHandle)).IsClosing() == 0 {
			(*libuv.Handle)(unsafe.Pointer(&c.pollHandle)).Close(nil)
		}

		if (*libuv.Handle)(unsafe.Pointer(&c.stream)).IsClosing() == 0 {
			(*libuv.Handle)(unsafe.Pointer(&c.stream)).Close(nil)
		}
	}
}

type HandlerFunc func(ResponseWriter, *Request)

func (f HandlerFunc) ServeHTTP(w ResponseWriter, r *Request) {
	f(w, r)
}

func NotFoundHandler() Handler { return HandlerFunc(NotFound) }

func NotFound(w ResponseWriter, r *Request) {
	w.WriteHeader(404)
	w.Write([]byte("404 page not found"))
}

// Error replies to the request with the specified error message and HTTP code.
// It does not otherwise end the request; the caller should ensure no further
// writes are done to w.
// The error message should be plain text.
//
// Error deletes the Content-Length header,
// sets Content-Type to “text/plain; charset=utf-8”,
// and sets X-Content-Type-Options to “nosniff”.
// This configures the header properly for the error message,
// in case the caller had set it up expecting a successful output.
func Error(w ResponseWriter, error string, code int) {
	h := w.Header()

	// Delete the Content-Length header, which might be for some other content.
	// Assuming the error string fits in the writer's buffer, we'll figure
	// out the correct Content-Length for it later.
	//
	// We don't delete Content-Encoding, because some middleware sets
	// Content-Encoding: gzip and wraps the ResponseWriter to compress on-the-fly.
	// See https://go.dev/issue/66343.
	h.Del("Content-Length")

	// There might be content type already set, but we reset it to
	// text/plain for the error message.
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	fmt.Fprintln(w, error)
}
