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

	uvLoop        *libuv.Loop
	uvServer      libuv.Tcp
	inShutdown    atomic.Bool
	http1Opts     *hyper.Http1ServerconnOptions
	http2Opts     *hyper.Http2ServerconnOptions
	checkHandle   libuv.Check

	mu                sync.Mutex
	activeConnections map[*conn]struct{}
}

type conn struct {
	Stream        libuv.Tcp
	PollHandle    libuv.Poll
	EventMask     c.Uint
	ReadWaker     *hyper.Waker
	WriteWaker    *hyper.Waker
	IsClosing     atomic.Bool
	ClosedHandles int32
	Executor      *hyper.Executor
}

type serviceUserdata struct {
	Host       [128]c.Char
	Port       [8]c.Char
	Conn       *conn
	Server     *Server
	ListenAddr string
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

	//(*libuv.Stream)(&srv.uvServer).Data = unsafe.Pointer(srv)
	(*libuv.Handle)(unsafe.Pointer(&srv.uvServer)).SetData(unsafe.Pointer(srv))
	if err := (*libuv.Stream)(&srv.uvServer).Listen(128, onNewConnection); err != 0 {
		return fmt.Errorf("failed to listen: %v", err)
	}

	if r := libuv.InitCheck(srv.uvLoop, &srv.checkHandle); r != 0 {
		fmt.Fprintf(os.Stderr, "Failed to initialize check handler: %d\n", r)
		os.Exit(1)
	}

	(*libuv.Handle)(unsafe.Pointer(&srv.checkHandle)).SetData(unsafe.Pointer(srv))

	if r := srv.checkHandle.Start(onCheck); r != 0 {
		fmt.Fprintf(os.Stderr, "Failed to start check handler: %d\n", r)
		os.Exit(1)
	}

	fmt.Printf("Listening on %s\n", srv.Addr)

	for {
		res := srv.uvLoop.Run(libuv.RUN_NOWAIT)
		if res < 0 {
			fmt.Fprintf(os.Stderr, "uv_loop_run error: %s\n", libuv.Strerror(libuv.Errno(res)))
			break
		}

		if srv.shuttingDown() {
			break
		}
	}
	return nil
}

func HandleFunc(pattern string, handler func(ResponseWriter, *Request)) {
	DefaultServeMux.HandleFunc(pattern, handler)
}

func onNewConnection(serverStream *libuv.Stream, status c.Int) {
	fmt.Println("onNewConnection called")
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

	libuv.InitTcp(srv.uvLoop, &conn.Stream)
	conn.Stream.Data = unsafe.Pointer(conn)

	if serverStream.Accept((*libuv.Stream)(unsafe.Pointer(&conn.Stream))) == 0 {
		fmt.Println("Accepted new connection")
		r := libuv.PollInit(srv.uvLoop, &conn.PollHandle, libuv.OsFd(conn.Stream.GetIoWatcherFd()))
		if r < 0 {
			fmt.Fprintf(os.Stderr, "uv_poll_init error: %s\n", libuv.Strerror(libuv.Errno(r)))
			(*libuv.Handle)(unsafe.Pointer(&conn.Stream)).Close(nil)
			return
		}
	
		(*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).Data = unsafe.Pointer(conn)
	
		if !updateConnRegistrations(conn, true) {
			(*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).Close(nil)
			(*libuv.Handle)(unsafe.Pointer(&conn.Stream)).Close(nil)
			return
		}

		userdata := createServiceUserdata()
		userdata.Server = srv
		if userdata == nil {
			fmt.Fprintf(os.Stderr, "Failed to create service userdata\n")
			(*libuv.Handle)(unsafe.Pointer(&conn.Stream)).Close(nil)
			return
		}
		fmt.Printf("ListenAddr: %s\n", srv.Addr)
		userdata.ListenAddr = srv.Addr

		var addr cnet.SockaddrStorage
		addrlen := c.Int(unsafe.Sizeof(addr))
		conn.Stream.Getpeername((*cnet.SockAddr)(c.Pointer(&addr)), &addrlen)

		if addr.Family == cnet.AF_INET {
			s := (*cnet.SockaddrIn)(unsafe.Pointer(&addr))
			libuv.Ip4Name(s, (*c.Char)(&userdata.Host[0]), unsafe.Sizeof(userdata.Host))
			c.Snprintf((*c.Char)(&userdata.Port[0]), unsafe.Sizeof(userdata.Port), c.Str("%d"), cnet.Ntohs(s.Port))
		} else if addr.Family == cnet.AF_INET6 {
			s := (*cnet.SockaddrIn6)(unsafe.Pointer(&addr))
			libuv.Ip6Name(s, (*c.Char)(&userdata.Host[0]), unsafe.Sizeof(userdata.Host))
			c.Snprintf((*c.Char)(&userdata.Port[0]), unsafe.Sizeof(userdata.Port), c.Str("%d"), cnet.Ntohs(s.Port))
		}

		executor := hyper.NewExecutor()
		if executor == nil {
			fmt.Fprintf(os.Stderr, "Failed to create Executor\n")
			(*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).Close(nil)
			(*libuv.Handle)(unsafe.Pointer(&conn.Stream)).Close(nil)
			return
		}
		conn.Executor = executor

		fmt.Println("Conn created")
		srv.trackConn(conn, true)
		fmt.Println("Conn tracked")

		userdata.Conn = conn

		io := createIo(conn)
		service := hyper.ServiceNew(serverCallback)
		service.SetUserdata(unsafe.Pointer(userdata), nil)

		http1Opts := hyper.Http1ServerconnOptionsNew(conn.Executor)
		if http1Opts == nil {
			fmt.Fprintf(os.Stderr, "Failed to create http1_opts\n")
			os.Exit(1)
		}
		result := http1Opts.HeaderReadTimeout(5 * 1000)
		if result != hyper.OK {
			fmt.Fprintf(os.Stderr, "Failed to set header read timeout for http1_opts\n")
			os.Exit(1)
		}
		srv.http1Opts = http1Opts
	
		http2Opts := hyper.Http2ServerconnOptionsNew(conn.Executor)
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
		srv.http2Opts = http2Opts

		serverconn := hyper.ServeHttpXConnection(http1Opts, http2Opts, io, service)
		conn.Executor.Push(serverconn)
	} else {
		fmt.Println("Client not accepted")
		(*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).Close(nil)
		(*libuv.Handle)(unsafe.Pointer(&conn.Stream)).Close(nil)
	}
}

func onCheck(handle *libuv.Check) {
	srv := (*Server)((*libuv.Handle)(unsafe.Pointer(handle)).GetData())
	for conn := range srv.activeConnections {
		if conn.Executor != nil {
			task := conn.Executor.Poll()
			for task != nil {
				srv.handleTask(task)
				task = conn.Executor.Poll()
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

	req, err := newRequest(userData.ListenAddr, userData.Conn, hyperReq)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		return
	}

	res := newResponse(channel)
	fmt.Printf("Response created\n")

	userData.Server.Handler.ServeHTTP(res, req)

	res.finalize()
}

func (srv *Server) handleTask(task *hyper.Task) {
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
	ret := cnet.Recv(conn.Stream.GetIoWatcherFd(), unsafe.Pointer(buf), bufLen, 0)

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
		fmt.Printf("ReadCb Event mask: %d\n", conn.EventMask)
		if !updateConnRegistrations(conn, false) {
			return hyper.IoError
		}
		fmt.Printf("ReadCb updateConnRegistrations\n")
	}

	conn.ReadWaker = ctx.Waker()
	return hyper.IoPending
}

func writeCb(userdata unsafe.Pointer, ctx *hyper.Context, buf *byte, bufLen uintptr) uintptr {
	conn := (*conn)(userdata)
	ret := cnet.Send(conn.Stream.GetIoWatcherFd(), unsafe.Pointer(buf), bufLen, 0)

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
		fmt.Printf("WriteCb Event mask: %d\n", conn.EventMask)
		if !updateConnRegistrations(conn, false) {
			return hyper.IoError
		}
	}

	conn.WriteWaker = ctx.Waker()
	return hyper.IoPending
}


func onPoll(handle *libuv.Poll, status c.Int, events c.Int) {
	fmt.Printf("onPoll called\n")
	conn := (*conn)((*libuv.Handle)(unsafe.Pointer(handle)).GetData())

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

func updateConnRegistrations(conn *conn, create bool) bool {
	fmt.Println("updateConnRegistrations called")

	events := c.Int(0)
	if conn.EventMask == 0 {
		fmt.Println("No events to poll, skipping poll start.")
		return true
	}
	fmt.Printf("Event mask: %d\n", conn.EventMask)
	if conn.EventMask&c.Uint(libuv.READABLE) != 0 {
		events |= c.Int(libuv.READABLE)
	}
	if conn.EventMask&c.Uint(libuv.WRITABLE) != 0 {
		events |= c.Int(libuv.WRITABLE)
	}

	fmt.Printf("Starting poll with events: %d\n", events)
	r := conn.PollHandle.Start(events, onPoll)
	//fmt.Println("Poll handle started: %d", r)
	if r < 0 {
		fmt.Fprintf(os.Stderr, "uv_poll_start error: %s\n", libuv.Strerror(libuv.Errno(r)))
		return false
	}
	fmt.Printf("Poll handle started: %d\n", r)
	return true
}

func createConnData() (*conn, error) {
	conn := &conn{}
	if conn == nil {
		return nil, fmt.Errorf("failed to allocate conn_data")
	}
	conn.IsClosing.Store(false)
	conn.ClosedHandles = 0

	return conn, nil
}

func freeConnData(userdata c.Pointer) {
	conn := (*conn)(userdata)
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
		if c.Executor != nil {
			c.Executor.Free()
		}
		delete(srv.activeConnections, c)
	}

	srv.uvLoop.Walk(closeWalkCb, nil)
	srv.uvLoop.Run(libuv.RUN_DEFAULT)

	srv.uvLoop.Close()

	if srv.http1Opts != nil {
        srv.http1Opts.Free()
    }
    if srv.http2Opts != nil {
        srv.http2Opts.Free()
    }
	return nil
}

func (s *Server) shuttingDown() bool {
	return s.inShutdown.Load()
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
