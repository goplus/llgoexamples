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

	uvLoop     *libuv.Loop
	uvServer   libuv.Tcp
	inShutdown atomic.Bool
	idleHandle libuv.Idle

	mu                sync.Mutex
	activeConnections map[*conn]struct{}
}

type conn struct {
	stream        libuv.Tcp
	pollHandle    libuv.Poll
	eventMask     c.Uint
	readWaker     *hyper.Waker
	writeWaker    *hyper.Waker
	http1Opts     *hyper.Http1ServerconnOptions
	http2Opts     *hyper.Http2ServerconnOptions
	isClosing     atomic.Bool
	closedHandles int32
	executor      *hyper.Executor
	remoteAddr    string
	requestBody   *requestBody
	asyncHandle   *libuv.Async
}

type serviceUserdata struct {
	host   [128]c.Char
	port   [8]c.Char
	conn   *conn
	server *Server
}

func NewServer(addr string) *Server {
	activeClients := make(map[*conn]struct{})
	return &Server{
		Addr:              addr,
		Handler:           DefaultServeMux,
		activeConnections: activeClients,
	}
}

// ErrServerClosed is returned by the [Server.Serve], [ServeTLS], [ListenAndServe],
// and [ListenAndServeTLS] methods after a call to [Server.Shutdown] or [Server.Close].
var ErrServerClosed = errors.New("http: Server closed")

func ListenAndServe(addr string, handler Handler) error {
	server := &Server{Addr: addr, Handler: handler}
	return server.ListenAndServe()
}

func (srv *Server) ListenAndServe() error {
	srv.uvLoop = libuv.DefaultLoop()
	if srv.uvLoop == nil {
		return fmt.Errorf("failed to get default loop")
	}

	if r := libuv.InitTcp(srv.uvLoop, &srv.uvServer); r != 0 {
		return fmt.Errorf("failed to init TCP: %v", libuv.Strerror(libuv.Errno(r)))
	}

	host, port, err := net.SplitHostPort(srv.Addr)
	if err != nil {
		return fmt.Errorf("invalid address %q: %v", srv.Addr, err)
	}

	portNum, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("invalid port number: %v", err)
	}

	var sockaddr cnet.SockaddrIn
	if r := libuv.Ip4Addr(c.AllocaCStr(host), c.Int(portNum), &sockaddr); r != 0 {
		return fmt.Errorf("failed to create IP address: %v", libuv.Strerror(libuv.Errno(r)))
	}

	if r := srv.uvServer.Bind((*cnet.SockAddr)(unsafe.Pointer(&sockaddr)), 0); r != 0 {
		return fmt.Errorf("failed to bind: %v", libuv.Strerror(libuv.Errno(r)))
	}

	// Set SO_REUSEADDR
	yes := c.Int(1)
	result := cnet.SetSockOpt(srv.uvServer.GetIoWatcherFd(), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, unsafe.Pointer(&yes), c.Uint(unsafe.Sizeof(yes)))
	if result != 0 {
		return fmt.Errorf("failed to set SO_REUSEADDR: %v", result)
	}

	srv.uvServer.Data = unsafe.Pointer(srv)
	if err := (*libuv.Stream)(&srv.uvServer).Listen(128, onNewConnection); err != 0 {
		return fmt.Errorf("failed to listen: %v", err)
	}

	if r := libuv.InitIdle(srv.uvLoop, &srv.idleHandle); r != 0 {
		fmt.Fprintf(os.Stderr, "Failed to initialize idle handler: %d\n", r)
		os.Exit(1)
	}

	(*libuv.Handle)(unsafe.Pointer(&srv.idleHandle)).SetData(unsafe.Pointer(srv))

	if r := srv.idleHandle.Start(onIdle); r != 0 {
		fmt.Fprintf(os.Stderr, "Failed to start idle handler: %d\n", r)
		os.Exit(1)
	}

	fmt.Printf("Listening on %s\n", srv.Addr)

	res := srv.uvLoop.Run(libuv.RUN_DEFAULT)
	if res != 0 {
		fmt.Fprintf(os.Stderr, "Error in event loop: %v\n", res)
		os.Exit(1)
	}

	return nil
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

	srv := (*Server)(serverStream.Data)
	if srv == nil {
		fmt.Fprintf(os.Stderr, "Server is nil\n")
		return
	}

	conn, err := createConnData()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create Conn: %v\n", err)
		return
	}

	fmt.Println("[debug] async handle creating")

	conn.asyncHandle = &libuv.Async{}
	srv.uvLoop.Async(conn.asyncHandle, onAsync)

	libuv.InitTcp(srv.uvLoop, &conn.stream)
	conn.stream.Data = unsafe.Pointer(conn)

	if serverStream.Accept((*libuv.Stream)(unsafe.Pointer(&conn.stream))) == 0 {
		fmt.Println("[debug] Accepted new connection")
		r := libuv.PollInit(srv.uvLoop, &conn.pollHandle, libuv.OsFd(conn.stream.GetIoWatcherFd()))
		if r < 0 {
			fmt.Fprintf(os.Stderr, "uv_poll_init error: %s\n", libuv.Strerror(libuv.Errno(r)))
			(*libuv.Handle)(unsafe.Pointer(&conn.stream)).Close(nil)
			return
		}

		(*libuv.Handle)(unsafe.Pointer(&conn.pollHandle)).Data = unsafe.Pointer(conn)

		if !updateConnRegistrations(conn, true) {
			(*libuv.Handle)(unsafe.Pointer(&conn.pollHandle)).Close(nil)
			(*libuv.Handle)(unsafe.Pointer(&conn.stream)).Close(nil)
			return
		}

		userdata := createServiceUserdata()
		userdata.server = srv
		if userdata == nil {
			fmt.Fprintf(os.Stderr, "Failed to create service userdata\n")
			(*libuv.Handle)(unsafe.Pointer(&conn.stream)).Close(nil)
			return
		}

		var addr cnet.SockaddrStorage
		addrlen := c.Int(unsafe.Sizeof(addr))
		conn.stream.Getpeername((*cnet.SockAddr)(c.Pointer(&addr)), &addrlen)

		if addr.Family == cnet.AF_INET {
			s := (*cnet.SockaddrIn)(unsafe.Pointer(&addr))
			libuv.Ip4Name(s, (*c.Char)(&userdata.host[0]), unsafe.Sizeof(userdata.host))
			c.Snprintf((*c.Char)(&userdata.port[0]), unsafe.Sizeof(userdata.port), c.Str("%d"), cnet.Ntohs(s.Port))
		} else if addr.Family == cnet.AF_INET6 {
			s := (*cnet.SockaddrIn6)(unsafe.Pointer(&addr))
			libuv.Ip6Name(s, (*c.Char)(&userdata.host[0]), unsafe.Sizeof(userdata.host))
			c.Snprintf((*c.Char)(&userdata.port[0]), unsafe.Sizeof(userdata.port), c.Str("%d"), cnet.Ntohs(s.Port))
		}

		conn.remoteAddr = c.GoString((*c.Char)(&userdata.host[0])) + ":" + c.GoString((*c.Char)(&userdata.port[0]))

		executor := hyper.NewExecutor()
		if executor == nil {
			fmt.Fprintf(os.Stderr, "Failed to create Executor\n")
			(*libuv.Handle)(unsafe.Pointer(&conn.pollHandle)).Close(nil)
			(*libuv.Handle)(unsafe.Pointer(&conn.stream)).Close(nil)
			return
		}
		conn.executor = executor

		fmt.Println("[debug] Conn created")
		srv.trackConn(conn, true)
		fmt.Println("[debug] Conn tracked")

		userdata.conn = conn

		io := createIo(conn)
		service := hyper.ServiceNew(serverCallback)
		service.SetUserdata(unsafe.Pointer(userdata), nil)
		http1Opts := hyper.Http1ServerconnOptionsNew(conn.executor)
		if http1Opts == nil {
			fmt.Fprintf(os.Stderr, "Failed to create http1_opts\n")
			os.Exit(1)
		}
		result := http1Opts.HeaderReadTimeout(5 * 1000)
		if result != hyper.OK {
			fmt.Fprintf(os.Stderr, "Failed to set header read timeout for http1_opts\n")
			os.Exit(1)
		}
		conn.http1Opts = http1Opts

		http2Opts := hyper.Http2ServerconnOptionsNew(conn.executor)
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
		conn.http2Opts = http2Opts

		serverconn := hyper.ServeHttpXConnection(http1Opts, http2Opts, io, service)
		conn.executor.Push(serverconn)
	} else {
		fmt.Println("[debug] Client not accepted")
		(*libuv.Handle)(unsafe.Pointer(&conn.pollHandle)).Close(nil)
		(*libuv.Handle)(unsafe.Pointer(&conn.stream)).Close(nil)
	}
}

func onAsync(asyncHandle *libuv.Async) {
	fmt.Println("[debug] onAsync called")
	taskData := (*taskData)(asyncHandle.GetData())
	dataTask := taskData.hyperBody.Data()
	dataTask.SetUserdata(c.Pointer(taskData), nil)
	if dataTask != nil {
		r := taskData.conn.executor.Push(dataTask)
		fmt.Printf("[debug] onAsync push data task: %d\n", r)
		if r != hyper.OK {
			fmt.Printf("failed to push data task: %d\n", r)
			dataTask.Free()
		}
	}
}

func onIdle(handle *libuv.Idle) {
	srv := (*Server)((*libuv.Handle)(unsafe.Pointer(handle)).GetData())
	for conn := range srv.activeConnections {
		if conn.executor != nil {
			task := conn.executor.Poll()
			for task != nil {
				srv.handleTask(task)
				task = conn.executor.Poll()
			}
		}
	}

	if srv.shuttingDown() {
		fmt.Println("Shutdown initiated, cleaning up...")
		handle.Stop()
	}
}

func serverCallback(userdata unsafe.Pointer, hyperReq *hyper.Request, channel *hyper.ResponseChannel) {
	userData := (*serviceUserdata)(userdata)

	if hyperReq == nil {
		fmt.Fprintf(os.Stderr, "Error: Received null request\n")
		return
	}

	req, err := userData.conn.readRequest(hyperReq)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		return
	}

	res := newResponse(channel)
	fmt.Println("[debug] Response created")

	//TODO(hackerchai): replace with no goroutine
	// userData.server.Handler.ServeHTTP(res, req)
	// res.finalize()
	go func() {
		userData.server.Handler.ServeHTTP(res, req)
		res.finalize()
	}()
}

func (srv *Server) handleTask(task *hyper.Task) {
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
			return
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
		payload.conn.requestBody.Close()
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
	payload.conn.requestBody.readCh <- bytes
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

func (s *Server) trackConn(c *conn, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeConnections == nil {
		s.activeConnections = make(map[*conn]struct{})
	}
	if add {
		s.activeConnections[c] = struct{}{}
	} else {
		delete(s.activeConnections, c)
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
		if !updateConnRegistrations(conn, false) {
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
		if !updateConnRegistrations(conn, false) {
			return hyper.IoError
		}
	}

	conn.writeWaker = ctx.Waker()
	return hyper.IoPending
}

func onPoll(handle *libuv.Poll, status c.Int, events c.Int) {
	fmt.Printf("[debug] onPoll called\n")
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

func updateConnRegistrations(conn *conn, create bool) bool {
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

	return conn, nil
}

func freeConnData(userdata c.Pointer) {
	conn := (*conn)(userdata)
	if conn != nil && !conn.isClosing.Swap(true) {
		fmt.Printf("[debug] Closing connection...\n")
		if conn.readWaker != nil {
			conn.readWaker.Free()
			conn.readWaker = nil
		}
		if conn.writeWaker != nil {
			conn.writeWaker.Free()
			conn.writeWaker = nil
		}

		if conn.executor != nil {
			conn.executor.Free()
			conn.executor = nil
		}

		if conn.http1Opts != nil {
			conn.http1Opts.Free()
			conn.http1Opts = nil
		}
		if conn.http2Opts != nil {
			conn.http2Opts.Free()
			conn.http2Opts = nil
		}

		if (*libuv.Handle)(unsafe.Pointer(&conn.pollHandle)).IsClosing() == 0 {
			(*libuv.Handle)(unsafe.Pointer(&conn.pollHandle)).Close(nil)
		}

		if (*libuv.Handle)(unsafe.Pointer(&conn.stream)).IsClosing() == 0 {
			(*libuv.Handle)(unsafe.Pointer(&conn.stream)).Close(nil)
		}
	}
}

func closeWalkCb(handle *libuv.Handle, arg c.Pointer) {
	if handle.IsClosing() == 0 {
		handle.Close(nil)
	}
}

func (srv *Server) Close() error {
	srv.inShutdown.Store(true)
	srv.mu.Lock()
	defer srv.mu.Unlock()

	for c := range srv.activeConnections {
		c.Close()

		delete(srv.activeConnections, c)
	}

	srv.uvLoop.Walk(closeWalkCb, nil)
	srv.uvLoop.Run(libuv.RUN_ONCE)
	(*libuv.Handle)(unsafe.Pointer(&srv.uvServer)).Close(nil)

	srv.uvLoop.Close()
	return nil
}

func (s *Server) shuttingDown() bool {
	return s.inShutdown.Load()
}

func (c *conn) shuttingDown() bool {
	return c.isClosing.Load()
}

func (c *conn) Close() {
	c.isClosing.Store(true)
	if c.shuttingDown() {
		return
	}

	if c.readWaker != nil {
		c.readWaker.Free()
		c.readWaker = nil
	}
	if c.writeWaker != nil {
		c.writeWaker.Free()
		c.writeWaker = nil
	}

	if c.executor != nil {
		c.executor.Free()
		c.executor = nil
	}
	if c.http1Opts != nil {
		c.http1Opts.Free()
		c.http1Opts = nil
	}
	if c.http2Opts != nil {
		c.http2Opts.Free()
		c.http2Opts = nil
	}

	(*libuv.Handle)(unsafe.Pointer(&c.pollHandle)).Close(nil)
	(*libuv.Handle)(unsafe.Pointer(&c.stream)).Close(nil)
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
