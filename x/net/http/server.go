package http

import (
	"fmt"
	"unsafe"

	"github.com/goplus/llgo/c/libuv"
	"github.com/goplus/llgo/c/net"
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

	uvLoop        *libuv.Loop
	uvServer      libuv.Tcp
	hyperExecutor *hyper.Executor
}

func NewServer(addr string) *Server {
	return &Server{
		Addr:    addr,
		Handler: DefaultServeMux,
	}
}

func (srv *Server) ListenAndServe() error {
	srv.uvLoop = libuv.DefaultLoop()
	srv.hyperExecutor = hyper.NewExecutor()

	if err := libuv.InitTcp(srv.uvLoop, &srv.uvServer); err != 0 {
		return fmt.Errorf("failed to init TCP: %v", err)
	}

	var sockaddr net.SockaddrIn
	if err := libuv.Ip4Addr(srv.Addr, 0, &sockaddr); err != 0 {
		return fmt.Errorf("failed to create IP address: %v", err)
	}

	if err := srv.uvServer.Bind((*net.SockAddr)(unsafe.Pointer(&sockaddr)), 0); err != 0 {
		return fmt.Errorf("failed to bind: %v", err)
	}

	if err := srv.uvServer.Listen(128, srv.onNewConnection); err != 0 {
		return fmt.Errorf("failed to listen: %v", err)
	}

	fmt.Printf("Listening on %s\n", srv.Addr)

	for {
		srv.uvLoop.Run(libuv.RUN_NOWAIT)

		task := srv.hyperExecutor.Poll()
		for task != nil {
			srv.handleTask(task)
			task.Free()
			task = srv.hyperExecutor.Poll()
		}
	}
}

func (srv *Server) onNewConnection(serverStream *libuv.Stream, status int) {
	if status < 0 {
		fmt.Printf("New connection error: %s\n", libuv.Strerror(libuv.Errno(status)))
		return
	}

	client := new(libuv.Tcp)
	libuv.InitTcp(srv.uvLoop, client)

	if serverStream.Accept((*libuv.Stream)(unsafe.Pointer(client))) == 0 {
		io := createIo(client)
		service := hyper.ServiceNew(srv.serverCallback)

		http1Opts := hyper.Http1ServerconnOptionsNew(srv.hyperExecutor)
		http2Opts := hyper.Http2ServerconnOptionsNew(srv.hyperExecutor)

		serverconn := hyper.ServeHttpXConnection(http1Opts, http2Opts, io, service)
		srv.hyperExecutor.Push(serverconn)

		http1Opts.Free()
		http2Opts.Free()
	} else {
		(*libuv.Handle)(unsafe.Pointer(client)).Close(nil)
	}
}

func (srv *Server) serverCallback(userdata unsafe.Pointer, hyperReq *hyper.Request, channel *hyper.ResponseChannel) {
	req, err := newRequest(hyperReq)
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
		fmt.Printf("Task error: %s\n", err.Message())
	}
}

func createIo(client *libuv.Tcp) *hyper.Io {
	io := hyper.NewIo()
	io.SetRead(func(userdata unsafe.Pointer, ctx *hyper.Context, buf *byte, bufLen uintptr) uintptr {
		ret := client.Read(unsafe.Pointer(buf), bufLen)
		if ret < 0 {
			return hyper.IoError
		}
		return uintptr(ret)
	})
	io.SetWrite(func(userdata unsafe.Pointer, ctx *hyper.Context, buf *byte, bufLen uintptr) uintptr {
		ret := client.Write(unsafe.Pointer(buf), bufLen)
		if ret < 0 {
			return hyper.IoError
		}
		return uintptr(ret)
	})
	return io
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