package http

import (
	"errors"
	"fmt"
	"io"
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
	"github.com/goplus/llgoexamples/rust/hyper"
	"github.com/goplus/llgoexamples/x/net"
)

// maxPostHandlerReadBytes is the max number of Request.Body bytes not
// consumed by a handler that the server will read from the client
// in order to keep a connection alive. If there are more bytes than
// this then the server to be paranoid instead sends a "Connection:
// close" response.
//
// This number is approximately what a typical machine's TCP buffer
// size is anyway.  (if we have the bytes on the machine, we might as
// well read them)
const maxPostHandlerReadBytes = 256 << 10

// _SC_NPROCESSORS_ONLN is the number of processors on the system
const _SC_NPROCESSORS_ONLN c.Int = 58

// DefaultChunkSize is the default chunk size for reading and writing data
var DefaultChunkSize uintptr = 8192

// cpuCount is the number of processors on the system
var cpuCount int

// Handler is the interface implemented by objects that can serve HTTP requests.
type Handler interface {
	ServeHTTP(ResponseWriter, *Request)
}

// ResponseWriter is the interface implemented by objects that can write HTTP responses.
type ResponseWriter interface {
	Header() Header
	Write([]byte) (int, error)
	WriteHeader(statusCode int)
}

// Server is a HTTP server.
type Server struct {
	Addr       string
	Handler    Handler
	isShutdown atomic.Bool

	eventLoop []*eventLoop
}

// eventLoop is a event loop for the server.
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

// conn is an abstraction of a connection.
type conn struct {
	stream        libuv.Tcp
	pollHandle    libuv.Poll
	eventMask     c.Uint
	readWaker     *hyper.Waker
	writeWaker    *hyper.Waker
	isClosing     atomic.Bool
	closedHandles int32
	remoteAddr    string
}

// serviceUserdata is the user data for the service.
type serviceUserdata struct {
	asyncHandle *libuv.Async
	host        [128]c.Char
	port        [8]c.Char
	executor    *hyper.Executor
}

// NewServer creates a new Server.
func NewServer(addr string) *Server {
	return &Server{
		Addr:    addr,
		Handler: DefaultServeMux,
	}
}

// newEventLoop creates a new event loop.
func newEventLoop() (*eventLoop, error) {
	activeClients := make(map[*conn]struct{})
	el := &eventLoop{
		activeConnections: activeClients,
	}

	// create executor
	executor := hyper.NewExecutor()
	if executor == nil {
		return nil, fmt.Errorf("failed to create Executor")
	}
	el.executor = executor

	// set http options
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

	// create libuv event loop
	el.uvLoop = libuv.LoopNew()
	if el.uvLoop == nil {
		return nil, fmt.Errorf("failed to get default loop")
	}

	// set event loop data
	el.uvLoop.SetData(unsafe.Pointer(el))

	// create libuv TCP server
	if r := libuv.InitTcpEx(el.uvLoop, &el.uvServer, cnet.AF_INET); r != 0 {
		return nil, fmt.Errorf("failed to init TCP: %s", c.GoString(libuv.Strerror(libuv.Errno(r))))
	}

	return el, nil
}

// run runs the event loop.
func (el *eventLoop) run(host string, port int) error {
	var sockaddr cnet.SockaddrIn
	if r := libuv.Ip4Addr(c.AllocaCStr(host), c.Int(port), &sockaddr); r != 0 {
		return fmt.Errorf("failed to create IP address: %s", c.GoString(libuv.Strerror(libuv.Errno(r))))
	}

	// set SO_REUSEADDR and SO_REUSEPORT for the server
	if err := setReuseAddr(&el.uvServer); err != nil {
		return fmt.Errorf("failed to set SO_REUSEADDR: %s", err)
	}

	// bind the server to the address
	if r := el.uvServer.Bind((*cnet.SockAddr)(unsafe.Pointer(&sockaddr)), 0); r != 0 {
		return fmt.Errorf("failed to bind: %s", c.GoString(libuv.Strerror(libuv.Errno(r))))
	}

	// listen for new connections
	if r := (*libuv.Stream)(&el.uvServer).Listen(128, onNewConnection); r != 0 {
		return fmt.Errorf("failed to listen: %s", c.GoString(libuv.Strerror(libuv.Errno(r))))
	}

	// create idle handler
	if r := libuv.InitIdle(el.uvLoop, &el.idleHandle); r != 0 {
		return fmt.Errorf("failed to initialize idle handler: %d", r)
	}

	// set idle handler data
	(*libuv.Handle)(unsafe.Pointer(&el.idleHandle)).SetData(unsafe.Pointer(el))

	// start the idle handler
	if r := el.idleHandle.Start(onIdle); r != 0 {
		return fmt.Errorf("failed to start idle handler: %d", r)
	}

	// run the libuv event loop
	if r := el.uvLoop.Run(libuv.RUN_DEFAULT); r != 0 {
		return fmt.Errorf("error in event loop: %d", r)
	}

	return nil
}

// setReuseAddr sets the SO_REUSEADDR and SO_REUSEPORT options for the given TCP handle.
func setReuseAddr(handle *libuv.Tcp) error {
	var fd libuv.OsFd
	result := (*libuv.Handle)(unsafe.Pointer(handle)).Fileno(&fd)
	if result != 0 {
		return fmt.Errorf("Error getting file descriptor")
	}

	yes := c.Int(1)
	// set SO_REUSEADDR
	if err := cnet.SetSockOpt(c.Int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, unsafe.Pointer(&yes), c.Uint(unsafe.Sizeof(yes))); err != 0 {
		return fmt.Errorf("Error setting SO_REUSEADDR")
	}

	// set SO_REUSEPORT
	if err := cnet.SetSockOpt(c.Int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, unsafe.Pointer(&yes), c.Uint(unsafe.Sizeof(yes))); err != 0 {
		return fmt.Errorf("Error setting SO_REUSEPORT")
	}

	return nil
}

// ErrServerClosed is returned by the [Server.Serve], [ServeTLS], [ListenAndServe],
// and [ListenAndServeTLS] methods after a call to [Server.Shutdown] or [Server.Close].
var ErrServerClosed = errors.New("http: Server closed")

// ListenAndServe listens on the TCP network address addr and then calls Serve
// to handle requests on incoming connections.
func ListenAndServe(addr string, handler Handler) error {
	server := &Server{Addr: addr, Handler: handler}
	return server.ListenAndServe()
}

// ListenAndServe listens on the TCP network address addr and then calls Serve
// to handle requests on incoming connections.
func (srv *Server) ListenAndServe() error {
	// get the number of processors on the system
	cpuCount = int(c.Sysconf(_SC_NPROCESSORS_ONLN))
	if cpuCount <= 0 {
		cpuCount = 4
	}

	// create event loops
	for i := 0; i < cpuCount; i++ {
		el, err := newEventLoop()
		if err != nil {
			return fmt.Errorf("failed to create event loop: %v", err)
		}
		srv.eventLoop = append(srv.eventLoop, el)
	}

	// parse the address
	host, port, err := net.SplitHostPort(srv.Addr)
	if err != nil {
		return fmt.Errorf("invalid address %q: %v", srv.Addr, err)
	}

	portNum, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("invalid port number: %v", err)
	}

	// create error channel and wait group
	errChan := make(chan error, len(srv.eventLoop))
	var wg sync.WaitGroup

	for _, el := range srv.eventLoop {
		wg.Add(1)
		go func(el *eventLoop) {
			err := el.run(host, portNum)
			if err != nil {
				errChan <- fmt.Errorf("failed to run event loop: %v", err)
			}
			wg.Done()
		}(el)
	}

	wg.Wait()

	// wait for all event loops to finish
	close(errChan)

	// collect errors from all event loops
	for err := range errChan {
		return fmt.Errorf("error in event loop: %v", err)
	}

	fmt.Printf("Listening on %s\n", srv.Addr)

	return nil
}

// HandleFunc is a convenience function to register a handler for a pattern.
func HandleFunc(pattern string, handler func(ResponseWriter, *Request)) {
	DefaultServeMux.HandleFunc(pattern, handler)
}

// onNewConnection is the libuv callback function for new connections.
func onNewConnection(serverStream *libuv.Stream, status c.Int) {
	if status < 0 {
		fmt.Printf("New connection error: %s\n", c.GoString(libuv.Strerror(libuv.Errno(status))))
		return
	}

	// get the event loop
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

	// create async handle for request to notify hyper executor reading next request chunk
	requestNotifyHandle := &libuv.Async{}
	el.uvLoop.Async(requestNotifyHandle, onAsync)

	// initialize the TCP connection
	libuv.InitTcp(el.uvLoop, &conn.stream)
	conn.stream.Data = unsafe.Pointer(conn)

	// accept the connection
	if serverStream.Accept((*libuv.Stream)(unsafe.Pointer(&conn.stream))) == 0 {
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

		userData.executor = el.executor
		userData.asyncHandle = requestNotifyHandle

		// get the remote address
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

		conn.remoteAddr = c.GoString((*c.Char)(&userData.host[0])) + ":" + c.GoString((*c.Char)(&userData.port[0]))

		r := libuv.PollInit(el.uvLoop, &conn.pollHandle, libuv.OsFd(conn.stream.GetIoWatcherFd()))
		if r < 0 {
			fmt.Fprintf(os.Stderr, "uv_poll_init error: %s\n", c.GoString(libuv.Strerror(libuv.Errno(r))))
			(*libuv.Handle)(unsafe.Pointer(&conn.stream)).Close(nil)
			return
		}

		(*libuv.Handle)(unsafe.Pointer(&conn.pollHandle)).Data = unsafe.Pointer(conn)

		// update the connection registrations
		if !updateConnRegistrations(conn) {
			(*libuv.Handle)(unsafe.Pointer(&conn.pollHandle)).Close(nil)
			(*libuv.Handle)(unsafe.Pointer(&conn.stream)).Close(nil)
			return
		}

		// track the connection
		el.trackConn(conn, true)

		// create hyper io
		io := createIo(conn)

		// create hyper service
		service := hyper.ServiceNew(serverCallback)
		service.SetUserdata(unsafe.Pointer(userData), freeServiceUserdata)

		serverConn := hyper.ServeHttpXConnection(el.http1Opts, el.http2Opts, io, service)
		el.executor.Push(serverConn)
	} else {
		(*libuv.Handle)(unsafe.Pointer(&conn.pollHandle)).Close(nil)
		(*libuv.Handle)(unsafe.Pointer(&conn.stream)).Close(nil)
	}
}

// onAsync is the libuv callback function for async events.
func onAsync(asyncHandle *libuv.Async) {
	taskData := (*serverTaskData)(asyncHandle.GetData())
	if taskData == nil {
		return
	}

	// set the task data to the hyper body
	dataTask := taskData.hyperBody.Data()
	dataTask.SetUserdata(c.Pointer(taskData), nil)

	// push the task to the hyper executor
	if dataTask != nil {
		r := taskData.executor.Push(dataTask)
		if r != hyper.OK {
			fmt.Printf("failed to push data task: %d\n", r)
			dataTask.Free()
		}
	}
}

// onIdle is the libuv callback function for running libuv event loop.
func onIdle(handle *libuv.Idle) {
	el := (*eventLoop)((*libuv.Handle)(unsafe.Pointer(handle)).GetLoop().GetData())
	if el.executor != nil {
		// poll the hyper executor for tasks
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

// serverCallback is the callback function for hyper server connections.
func serverCallback(userData unsafe.Pointer, hyperReq *hyper.Request, channel *hyper.ResponseChannel) {
	// get the service userdata
	payload := (*serviceUserdata)(userData)
	if payload == nil {
		fmt.Fprintf(os.Stderr, "Error: Received null userData\n")
		return
	}

	executor := payload.executor
	if executor == nil {
		fmt.Fprintf(os.Stderr, "Error: Received null executor\n")
		return
	}

	requestNotifyHandle := payload.asyncHandle
	if requestNotifyHandle == nil {
		fmt.Fprintf(os.Stderr, "Error: Received null asyncHandle\n")
		return
	}

	host := payload.host
	port := payload.port

	if hyperReq == nil {
		fmt.Fprintf(os.Stderr, "Error: Received null request\n")
		return
	}

	remoteAddr := c.GoString(&host[0]) + ":" + c.GoString(&port[0])

	// read the request
	req, err := readRequest(executor, hyperReq, requestNotifyHandle, remoteAddr)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		return
	}

	// create a new response
	res := newResponse(channel)

	// handle the request
	DefaultServeMux.ServeHTTP(res, req)

	// finalize the response
	res.finalize()

	//TODO(hackerchai): replace with goroutine to enable blocking operation in handler
	// go func() {
	// 	DefaultServeMux.ServeHTTP(res, req)
	// 	res.finalize()
	// }()
}

// handleTask is the callback function for hyper tasks.
func handleTask(task *hyper.Task) {
	hyperTaskType := task.Type()

	// get the server task data
	payload := (*serverTaskData)(task.Userdata())

	// handle the task based on the task flag
	if payload != nil {
		switch payload.taskFlag {
		case getBodyTask:
			handleGetBodyTask(hyperTaskType, task, payload)
		case setBodyTask:
			handleSetBodyTask(hyperTaskType, task)
			return
		default:
			return
		}
	}

	// if the payload is nil, handle the task based on the task type
	switch hyperTaskType {
	case hyper.TaskError:
		handleTaskError(task)
		return
	case hyper.TaskEmpty:
		task.Free()
		return
	case hyper.TaskServerconn:
		task.Free()
		return
	default:
		return
	}
}

// handleGetBodyTask is the callback function for hyper tasks with get body task type.
func handleGetBodyTask(hyperTaskType hyper.TaskReturnType, task *hyper.Task, payload *serverTaskData) {
	switch hyperTaskType {
	case hyper.TaskError:
		handleTaskError(task)
	case hyper.TaskBuf:
		handleTaskBuffer(task, payload)
	case hyper.TaskEmpty:
		if payload.bodyStream != nil {
			payload.bodyStream.closeWithError(io.EOF)
		}
		task.Free()
	}
}

// handleSetBodyTask is the callback function for hyper tasks with set body task type.
func handleSetBodyTask(hyperTaskType hyper.TaskReturnType, task *hyper.Task) {
	switch hyperTaskType {
	case hyper.TaskError:
		handleTaskError(task)
	case hyper.TaskEmpty:
		task.Free()
	}
}

// handleTaskError is the callback function for hyper tasks with error task type.
func handleTaskError(task *hyper.Task) {
	err := (*hyper.Error)(task.Value())
	fmt.Printf("Error code: %d\n", err.Code())

	var errbuf [256]byte
	errlen := err.Print(&errbuf[0], unsafe.Sizeof(errbuf))
	fmt.Printf("Details: %s\n", errbuf[:errlen])
	err.Free()
	task.Free()
}

// handleTaskBuffer is the callback function for hyper tasks with buffer task type.
func handleTaskBuffer(task *hyper.Task, payload *serverTaskData) {
	buf := (*hyper.Buf)(task.Value())
	bytes := unsafe.Slice(buf.Bytes(), buf.Len())

	// push the bytes to the body stream
	payload.bodyStream.readCh <- bytes

	// free the buffer and the task
	buf.Free()
	task.Free()
}

// getTaskTypeString is the helper function for getting task type string.
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

// trackConn is the helper function for tracking connections into event loop.
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

// createIo is the helper function for creating hyper io.
func createIo(conn *conn) *hyper.Io {
	io := hyper.NewIo()
	io.SetUserdata(unsafe.Pointer(conn), freeConnData)
	io.SetRead(readCb)
	io.SetWrite(writeCb)
	return io
}

// createServiceUserdata is the helper function for creating service userdata.
func createServiceUserdata() *serviceUserdata {
	userdata := (*serviceUserdata)(c.Calloc(1, unsafe.Sizeof(serviceUserdata{})))
	if userdata == nil {
		fmt.Fprintf(os.Stderr, "Failed to allocate service_userdata\n")
	}
	return userdata
}

// freeServiceUserdata is the helper function for freeing service userdata.
func freeServiceUserdata(userdata c.Pointer) {
	castUserdata := (*serviceUserdata)(userdata)
	if castUserdata != nil {
		c.Free(c.Pointer(castUserdata))
	}
}

// readCb is the callback function for hyper io read.
func readCb(userdata unsafe.Pointer, ctx *hyper.Context, buf *byte, bufLen uintptr) uintptr {
	conn := (*conn)(userdata)
	ret := cnet.Recv(conn.stream.GetIoWatcherFd(), unsafe.Pointer(buf), bufLen, 0)

	// if the ret is greater than 0, return the ret
	if ret >= 0 {
		return uintptr(ret)
	}

	// if the ret is less than 0, return the IoError
	if uintptr(cos.Errno) != syscall.EAGAIN && uintptr(cos.Errno) != syscall.EWOULDBLOCK {
		return hyper.IoError
	}

	// if the readWaker is not nil, free the readWaker
	if conn.readWaker != nil {
		conn.readWaker.Free()
	}

	// if the eventMask is not readable, set the eventMask to readable
	if conn.eventMask&c.Uint(libuv.READABLE) == 0 {
		conn.eventMask |= c.Uint(libuv.READABLE)
		if !updateConnRegistrations(conn) {
			return hyper.IoError
		}
	}

	conn.readWaker = ctx.Waker()
	return hyper.IoPending
}

// writeCb is the callback function for hyper io write.
func writeCb(userdata unsafe.Pointer, ctx *hyper.Context, buf *byte, bufLen uintptr) uintptr {
	conn := (*conn)(userdata)
	ret := cnet.Send(conn.stream.GetIoWatcherFd(), unsafe.Pointer(buf), bufLen, 0)

	// if the ret is greater than 0, return the ret
	if ret >= 0 {
		return uintptr(ret)
	}

	// if the ret is less than 0, return the IoError
	if uintptr(cos.Errno) != syscall.EAGAIN && uintptr(cos.Errno) != syscall.EWOULDBLOCK {
		return hyper.IoError
	}

	// if the writeWaker is not nil, free the writeWaker
	if conn.writeWaker != nil {
		conn.writeWaker.Free()
	}

	// if the eventMask is not writable, set the eventMask to writable
	if conn.eventMask&c.Uint(libuv.WRITABLE) == 0 {
		conn.eventMask |= c.Uint(libuv.WRITABLE)
		if !updateConnRegistrations(conn) {
			return hyper.IoError
		}
	}

	conn.writeWaker = ctx.Waker()
	return hyper.IoPending
}

// onPoll is the callback function for libuv poll.
func onPoll(handle *libuv.Poll, status c.Int, events c.Int) {
	// get the conn
	conn := (*conn)((*libuv.Handle)(unsafe.Pointer(handle)).GetData())

	// if the status is less than 0, return the PollError
	if status < 0 {
		fmt.Fprintf(os.Stderr, "Poll error: %s\n", c.GoString(libuv.Strerror(libuv.Errno(status))))
		return
	}

	// if the events is readable and the readWaker is not nil, wake the readWaker
	if events&c.Int(libuv.READABLE) != 0 && conn.readWaker != nil {
		conn.readWaker.Wake()
		conn.readWaker = nil
	}

	// if the events is writable and the writeWaker is not nil, wake the writeWaker
	if events&c.Int(libuv.WRITABLE) != 0 && conn.writeWaker != nil {
		conn.writeWaker.Wake()
		conn.writeWaker = nil
	}
}

// updateConnRegistrations is the helper function for updating connection registrations.
func updateConnRegistrations(conn *conn) bool {
	// initialize the events
	events := c.Int(0)

	// if the eventMask is 0, return true
	if conn.eventMask == 0 {
		return true
	}

	// if the eventMask is readable, set the events to readable
	if conn.eventMask&c.Uint(libuv.READABLE) != 0 {
		events |= c.Int(libuv.READABLE)
	}

	// if the eventMask is writable, set the events to writable
	if conn.eventMask&c.Uint(libuv.WRITABLE) != 0 {
		events |= c.Int(libuv.WRITABLE)
	}

	// start the poll
	r := conn.pollHandle.Start(events, onPoll)
	if r < 0 {
		fmt.Fprintf(os.Stderr, "uv_poll_start error: %s\n", c.GoString(libuv.Strerror(libuv.Errno(r))))
		return false
	}
	return true
}

// createConnData is the helper function for creating connection data.
func createConnData() (*conn, error) {
	conn := &conn{}
	if conn == nil {
		return nil, fmt.Errorf("failed to allocate conn_data")
	}
	conn.isClosing.Store(false)
	conn.closedHandles = 0

	return conn, nil
}

// freeConnData is the helper function for freeing connection data.
func freeConnData(userdata c.Pointer) {
	conn := (*conn)(userdata)
	conn.Close()
}

// closeWalkCb is the callback function for libuv handle close.
func closeWalkCb(handle *libuv.Handle, arg c.Pointer) {
	if handle.IsClosing() == 0 {
		handle.Close(nil)
	}
}

// Close is the method for closing the server.
func (srv *Server) Close() error {
	srv.isShutdown.Store(true)

	for _, el := range srv.eventLoop {
		el.Close()
	}

	return nil
}

// shuttingDown is the method for checking if the server is shutting down.
func (s *Server) shuttingDown() bool {
	return s.isShutdown.Load()
}

// Close is the method for closing the event loop.
func (el *eventLoop) Close() error {
	el.isShutdown.Store(true)

	for c := range el.activeConnections {
		c.Close()
		el.trackConn(c, false)
	}

	if el.executor != nil {
		el.executor.Free()
		el.executor = nil
	}
	if el.http1Opts != nil {
		el.http1Opts.Free()
		el.http1Opts = nil
	}
	if el.http2Opts != nil {
		el.http2Opts.Free()
		el.http2Opts = nil
	}

	el.uvLoop.Walk(closeWalkCb, nil)
	el.uvLoop.Run(libuv.RUN_ONCE)
	(*libuv.Handle)(unsafe.Pointer(&el.uvServer)).Close(nil)
	(*libuv.Handle)(unsafe.Pointer(&el.idleHandle)).Close(nil)
	el.uvLoop.Close()

	return nil
}

// shuttingDown is the method for checking if the event loop is shutting down.
func (el *eventLoop) shuttingDown() bool {
	return el.isShutdown.Load()
}

// Close is the method for closing the connection.
func (c *conn) Close() {
	if c != nil && !c.isClosing.Swap(true) {
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

// shuttingDown is the method for checking if the connection is shutting down.
func (c *conn) shuttingDown() bool {
	return c.isClosing.Load()
}

// HandlerFunc is the type for handler function.
type HandlerFunc func(ResponseWriter, *Request)

// ServeHTTP is the method for serving HTTP.
func (f HandlerFunc) ServeHTTP(w ResponseWriter, r *Request) {
	f(w, r)
}

// NotFoundHandler is the method for not found handler.
func NotFoundHandler() Handler { return HandlerFunc(NotFound) }

// NotFound is the method for not found.
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
