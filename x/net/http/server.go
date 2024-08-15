package http

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/libuv"
	"github.com/goplus/llgo/c/net"
	cos "github.com/goplus/llgo/c/os"
	"github.com/goplus/llgo/c/syscall"
	"github.com/goplus/llgo/rust/hyper"
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

	uvLoop   *libuv.Loop
	uvServer libuv.Tcp
	inShutdown atomic.Bool

	mu                sync.Mutex
	activeConnections map[*Conn]struct{}
}

type Conn struct {
	Stream     *libuv.Tcp
	PollHandle *libuv.Poll
	EventMask  c.Uint
	ReadWaker  *hyper.Waker
	WriteWaker *hyper.Waker
	ConnTask   *hyper.Task
	IsClosing  c.Int
	Executor   *hyper.Executor
}

func NewServer(addr string) *Server {
	return &Server{
		Addr:    addr,
		Handler: DefaultServeMux,
	}
}

func (srv *Server) ListenAndServe() error {
	srv.uvLoop = libuv.DefaultLoop()

	if err := libuv.InitTcp(srv.uvLoop, &srv.uvServer); err != 0 {
		return fmt.Errorf("failed to init TCP: %v", err)
	}

	var sockaddr net.SockaddrIn
	if err := libuv.Ip4Addr(c.AllocaCStr(srv.Addr), 0, &sockaddr); err != 0 {
		return fmt.Errorf("failed to create IP address: %v", err)
	}

	if err := srv.uvServer.Bind((*net.SockAddr)(unsafe.Pointer(&sockaddr)), 0); err != 0 {
		return fmt.Errorf("failed to bind: %v", err)
	}

	// Set SO_REUSEADDR
	yes := c.Int(1)
	result := net.SetSockOpt(srv.uvServer.GetIoWatcherFd(), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, unsafe.Pointer(&yes), c.Uint(unsafe.Sizeof(yes)))
	if result != 0 {
		return fmt.Errorf("failed to set SO_REUSEADDR: %v", result)
	}

	if err := (*libuv.Stream)(&srv.uvServer).Listen(128, srv.onNewConnection); err != 0 {
		return fmt.Errorf("failed to listen: %v", err)
	}

	fmt.Printf("Listening on %s\n", srv.Addr)

	for {
		srv.uvLoop.Run(libuv.RUN_NOWAIT)

		for conn := range srv.activeConnections {
			task := conn.Executor.Poll()
			for task != nil {
				srv.handleTask(task)
				task.Free()
				task = conn.Executor.Poll()
			}
		}
	}
}

func (srv *Server) onNewConnection(serverStream *libuv.Stream, status c.Int) {
	if status < 0 {
		fmt.Printf("New connection error: %s\n", libuv.Strerror(libuv.Errno(status)))
		return
	}

	client := new(libuv.Tcp)
	libuv.InitTcp(srv.uvLoop, client)

	if serverStream.Accept((*libuv.Stream)(unsafe.Pointer(client))) == 0 {
		conn := createConnData(srv.uvLoop, client)
		if conn == nil {
			fmt.Fprintf(os.Stderr, "Failed to create Conn\n")
			(*libuv.Handle)(unsafe.Pointer(client)).Close(onClose)
			return
		}
		srv.trackConn(conn, true)

		io := createIo(conn)
		service := hyper.ServiceNew(srv.serverCallback)
		service.SetUserdata(unsafe.Pointer(conn), freeConnData)

		http1Opts := hyper.Http1ServerconnOptionsNew(conn.Executor)
		http2Opts := hyper.Http2ServerconnOptionsNew(conn.Executor)

		serverconn := hyper.ServeHttpXConnection(http1Opts, http2Opts, io, service)
		conn.Executor.Push(serverconn)

		http1Opts.Free()
		http2Opts.Free()
	} else {
		(*libuv.Handle)(unsafe.Pointer(client)).Close(nil)
	}
}

func (srv *Server) serverCallback(userdata unsafe.Pointer, hyperReq *hyper.Request, channel *hyper.ResponseChannel) {
	conn := (*Conn)(userdata)

	if hyperReq == nil {
		fmt.Fprintf(os.Stderr, "Error: Received null request\n")
		return
	}

	req, err := newRequest(conn, hyperReq)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		return
	}

	res := newResponse(channel)

	srv.Handler.ServeHTTP(res, req)

	res.finalize()
}

func (srv *Server) handleTask(task *hyper.Task) {
	switch task.Type() {
	case hyper.TaskServerconn:
		fmt.Println("New server connection")
	case hyper.TaskResponse:
		fmt.Println("Response sent")
	case hyper.TaskError:
		err := (*hyper.Error)(task.Value())
		var errbuf [256]byte
		errlen := err.Print(&errbuf[0], unsafe.Sizeof(errbuf))
		fmt.Printf("Task error: %.*s\n", errlen, (*c.Char)(unsafe.Pointer(&errbuf[0])))
		err.Free()
	}
}

func (s *Server) trackConn(c *Conn, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeConnections == nil {
		s.activeConnections = make(map[*Conn]struct{})
	}
	if add {
		s.activeConnections[c] = struct{}{}
	} else {
		delete(s.activeConnections, c)
	}
}

func (srv *Server) Close() error {
	srv.inShutdown.Store(true)
	srv.mu.Lock()
	defer srv.mu.Unlock()

	for c := range srv.activeConnections {
		delete(srv.activeConnections, c)
	}
	return nil
}

func createIo(conn *Conn) *hyper.Io {
	io := hyper.NewIo()
	io.SetUserdata(unsafe.Pointer(conn), freeConnData)
	io.SetRead(readCb)
	io.SetWrite(writeCb)
	return io
}

func readCb(userdata unsafe.Pointer, ctx *hyper.Context, buf *byte, bufLen uintptr) uintptr {
	conn := (*Conn)(userdata)
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
		if !updateConnRegistrations(conn, false) {
			return hyper.IoError
		}
	}

	conn.ReadWaker = ctx.Waker()
	return hyper.IoPending
}

func writeCb(userdata unsafe.Pointer, ctx *hyper.Context, buf *byte, bufLen uintptr) uintptr {
	conn := (*Conn)(userdata)
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
		if !updateConnRegistrations(conn, false) {
			return hyper.IoError
		}
	}

	conn.WriteWaker = ctx.Waker()
	return hyper.IoPending
}

func onClose(handle *libuv.Handle) {
	c.Free(unsafe.Pointer(handle))
}

func onPoll(handle *libuv.Poll, status c.Int, events c.Int) {
	conn := (*Conn)((*libuv.Handle)(unsafe.Pointer(handle)).GetData())

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

func updateConnRegistrations(conn *Conn, create bool) bool {
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

func createConnData(loop *libuv.Loop, client *libuv.Tcp) *Conn {
	conn := (*Conn)(c.Calloc(1, unsafe.Sizeof(Conn{})))
	if conn == nil {
		fmt.Fprintf(os.Stderr, "Failed to allocate conn_data\n")
		return nil
	}
	c.Memcpy(unsafe.Pointer(&conn.Stream), unsafe.Pointer(client), unsafe.Sizeof(libuv.Tcp{}))
	conn.IsClosing = 0

	r := libuv.PollInit(loop, conn.PollHandle, libuv.OsFd(client.GetIoWatcherFd()))
	if r < 0 {
		fmt.Fprintf(os.Stderr, "uv_poll_init error: %s\n", libuv.Strerror(libuv.Errno(r)))
		c.Free(unsafe.Pointer(conn))
		return nil
	}

	(*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).Data = unsafe.Pointer(conn)
	conn.Stream.Data = unsafe.Pointer(conn)

	if !updateConnRegistrations(conn, true) {
		(*libuv.Handle)(unsafe.Pointer(&conn.PollHandle)).Close(nil)
		c.Free(unsafe.Pointer(conn))
		return nil
	}

	return conn
}

func freeConnData(userdata c.Pointer) {
	conn := (*Conn)(userdata)
	if conn != nil && conn.IsClosing == 0 {
		conn.IsClosing = 1
		// We don't immediately close the connection here.
		// Instead, we'll let the main loop handle the closure when appropriate.
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
