package libuv

import (
	"unsafe"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/libuv"
	"github.com/goplus/llgo/c/net"
)

type Handle struct {
	libuv.Handle
	ConnectionCb ConnectionCb
	ReadCb       ReadCb
	AllocCb      AllocCb
	CloseCb      CloseCb
	UdpRecvCb    UdpRecvCb
}

type Stream struct {
	libuv.Stream
	ConnectionCb ConnectionCb
	ReadCb       ReadCb
	AllocCb      AllocCb
	CloseCb      CloseCb
}

type Req struct {
	libuv.Req
	ConnectCb ConnectCb
	WriteCb   WriteCb
}

type Write struct {
	_Write    libuv.Write
	ConnectCb ConnectCb
	WriteCb   WriteCb
}

type Connect struct {
	libuv.Connect
	ConnectCb ConnectCb
	WriteCb   WriteCb
}

type Tcp struct {
	libuv.Tcp
	ConnectionCb ConnectionCb
	ReadCb       ReadCb
	AllocCb      AllocCb
	CloseCb      CloseCb
}

type Udp struct {
	libuv.Udp
	ConnectionCb ConnectionCb
	ReadCb       ReadCb
	AllocCb      AllocCb
	CloseCb      CloseCb
	UdpRecvCb    UdpRecvCb
}

type UdpSend struct {
	libuv.UdpSend
	UdpSendCb UdpSendCb
}

type GetAddrInfo struct {
	libuv.GetAddrInfo
	GetaddrinfoCb GetaddrinfoCb
}

type GetNameInfo struct {
	libuv.GetNameInfo
	GetnameinfoCb GetnameinfoCb
}

type Membership struct {
	libuv.Membership
}

type Shutdown struct {
	libuv.Shutdown
	ShutdownCb ShutdownCb
}

// ----------------------------------------------

/* Function type */

type ConnectionCb func(server *Stream, status c.Int)

type ReadCb func(stream *Stream, nread c.Long, buf *Buf)

type WriteCb func(req *Write, status c.Int)

type CloseCb func(handle *Handle)

type ConnectCb func(req *Connect, status c.Int)

type UdpSendCb func(req *UdpSend, status c.Int)

type AllocCb func(handle *Handle, suggestedSize uintptr, buf *Buf)

type UdpRecvCb func(handle *Udp, nread c.Long, buf *Buf, addr *net.SockAddr, flags c.Uint)

type GetaddrinfoCb func(req *GetAddrInfo, status c.Int, res *net.AddrInfo)

type GetnameinfoCb func(req *GetNameInfo, status c.Int, hostname *c.Char, service *c.Char)

type ShutdownCb func(req *Shutdown, status c.Int)

// ----------------------------------------------

/* Handle related functions and method. */

// Ref references the handle.
func (h *Handle) Ref() {
	(&h.Handle).Ref()
}

// Unref unreferences the handle.
func (h *Handle) Unref() {
	(&h.Handle).Unref()
}

// HasRef returns true if the handle has a reference.
func (h *Handle) HasRef() int {
	return int((&h.Handle).HasRef())
}

// HandleSize returns the size of the handle.
func HandleSize(handleType libuv.HandleType) uintptr {
	return libuv.HandleSize(handleType)
}

// GetType returns the type of the handle.
func (h *Handle) GetType() libuv.HandleType {
	return (&h.Handle).GetType()
}

// HandleTypeName returns the name of the handle type.
func HandleTypeName(handleType libuv.HandleType) string {
	return c.GoString(libuv.HandleTypeName(handleType))
}

// GetData returns the data of the handle.
func (h *Handle) GetData() c.Pointer {
	return (&h.Handle).GetData()
}

// GetLoop returns the loop of the handle.
func (h *Handle) GetLoop() *Loop {
	return &Loop{Loop: (&h.Handle).GetLoop()}
}

// SetData sets the data of the handle.
func (h *Handle) SetData(data c.Pointer) {
	(&h.Handle).SetData(data)
}

// IsActive returns true if the handle is active.
func (h *Handle) IsActive() int {
	return int((&h.Handle).IsActive())
}

// Close closes the handle.
func (h *Handle) Close(closeCb CloseCb) {
	h.CloseCb = closeCb
	(&h.Handle).Close(func(_handle *libuv.Handle) {
		handle := (*Handle)(unsafe.Pointer(_handle))
		handle.CloseCb(handle)
	})
}

// SendBufferSize returns the send buffer size of the handle.
func (h *Handle) SendBufferSize(value *c.Int) int {
	return int((&h.Handle).SendBufferSize(value))
}

// RecvBufferSize returns the receive buffer size of the handle.
func (h *Handle) RecvBufferSize(value *c.Int) int {
	return int((&h.Handle).RecvBufferSize(value))
}

// Fileno returns the file number of the handle.
func (h *Handle) Fileno(fd *libuv.OsFd) int {
	return int((&h.Handle).Fileno(fd))
}

// Pipe creates a new pipe.
func Pipe(fds [2]libuv.File, readFlags int, writeFlags int) int {
	return int(libuv.Pipe(fds, c.Int(readFlags), c.Int(writeFlags)))
}

// Socketpair creates a new socket pair.
func Socketpair(_type int, protocol int, socketVector [2]libuv.OsSock, flag0 int, flag1 int) int {
	return int(libuv.Socketpair(c.Int(_type), c.Int(protocol), socketVector, c.Int(flag0), c.Int(flag1)))
}

// IsClosing returns true if the handle is closing.
func (h *Handle) IsClosing() int {
	return int((&h.Handle).IsClosing())
}

// ----------------------------------------------

/* Req related functions and method. */

// ReqSize returns the size of the request.
func ReqSize(reqType libuv.ReqType) uintptr {
	return libuv.ReqSize(reqType)
}

// GetData returns the data of the request.
func (req *Req) GetData() c.Pointer {
	return (&req.Req).GetData()
}

// SetData sets the data of the request.
func (req *Req) SetData(data c.Pointer) {
	(&req.Req).SetData(data)
}

// GetType returns the type of the request.
func (req *Req) GetType() libuv.ReqType {
	return (&req.Req).GetType()
}

// TypeName returns the name of the request type.
func TypeName(reqType libuv.ReqType) string {
	return c.GoString(libuv.TypeName(reqType))
}

// ----------------------------------------------

/* Stream related functions and method. */

// GetWriteQueueSize returns the size of the write queue.
func (s *Stream) GetWriteQueueSize() uintptr {
	return (&s.Stream).GetWriteQueueSize()
}

// Listen listens to the stream.
func (s *Stream) Listen(backlog int, connectionCb ConnectionCb) int {
	s.ConnectionCb = connectionCb
	return int(s.Stream.Listen(c.Int(backlog), func(_server *libuv.Stream, status c.Int) {
		server := (*Stream)(unsafe.Pointer(_server))
		//server := &Stream{Stream: _server}
		server.ConnectionCb(server, status)
	}))
}

// Accept accepts the stream.
func (s *Stream) Accept(client *Stream) int {
	return int((&s.Stream).Accept(&client.Stream))
}

// StartRead starts reading from the stream.
func (s *Stream) StartRead(allocCb AllocCb, readCb ReadCb) int {
	s.AllocCb = allocCb
	s.ReadCb = readCb
	return int((&s.Stream).StartRead(func(_handle *libuv.Handle, suggestedSize uintptr, _buf *libuv.Buf) {
		stream := (*Stream)(unsafe.Pointer(_handle))
		buf := &Buf{Buf: _buf}
		stream.AllocCb((*Handle)(unsafe.Pointer(_handle)), suggestedSize, buf)
	}, func(_stream *libuv.Stream, nread c.Long, _buf *libuv.Buf) {
		stream := (*Stream)(unsafe.Pointer(_stream))
		buf := &Buf{Buf: _buf}
		stream.ReadCb(stream, nread, buf)
	}))
}

// StopRead stops reading from the stream.
func (s *Stream) StopRead() int {
	return int((&s.Stream).StopRead())
}

// NewWrite creates a new write request.
func NewWrite() *Write {
	write := libuv.Write{}
	return &Write{_Write: write}
}

// Write writes to the stream.
func (w *Write) Write(stream *Stream, bufs *Buf, nbufs int, writeCb WriteCb) int {
	w.WriteCb = writeCb
	return int((&w._Write).Write(&stream.Stream, bufs.Buf, c.Uint(nbufs), func(_req *libuv.Write, status c.Int) {
		req := (*Write)(unsafe.Pointer(_req))
		req.WriteCb(req, status)
	}))
}

// Write2 writes to the stream.
func (w *Write) Write2(stream *Stream, bufs *Buf, nbufs int, sendStream *Stream, writeCb WriteCb) int {
	w.WriteCb = writeCb
	return int((&w._Write).Write2(&stream.Stream, bufs.Buf, c.Uint(nbufs), &sendStream.Stream, func(_req *libuv.Write, status c.Int) {
		req := (*Write)(unsafe.Pointer(_req))
		req.WriteCb(req, status)
	}))
}

// TryWrite tries to write to the stream.
func (s *Stream) TryWrite(bufs *Buf, nbufs uint) int {
	return int((&s.Stream).TryWrite(bufs.Buf, c.Uint(nbufs)))
}

// TryWrite2 tries to write to the stream.
func (s *Stream) TryWrite2(bufs *Buf, nbufs uint, sendStream *Stream) int {
	return int((&s.Stream).TryWrite2(bufs.Buf, c.Uint(nbufs), &sendStream.Stream))
}

// IsReadable returns true if the stream is readable.
func (s *Stream) IsReadable() int {
	return int((&s.Stream).IsReadable())
}

// IsWritable returns true if the stream is writable.
func (s *Stream) IsWritable() int {
	return int((&s.Stream).IsWritable())
}

// SetBlocking sets the blocking status of the stream.
func (s *Stream) SetBlocking(blocking int) int {
	return int((&s.Stream).SetBlocking(c.Int(blocking)))
}

// StreamShutdown closes the stream.
func StreamShutdown(req *Shutdown, stream *Stream, shutdownCb ShutdownCb) int {
	req.ShutdownCb = shutdownCb
	return int(libuv.StreamShutdown(&req.Shutdown, &stream.Stream, func(_req *libuv.Shutdown, status c.Int) {
		req := (*Shutdown)(unsafe.Pointer(_req))
		req.ShutdownCb(req, status)
	}))
}

// ----------------------------------------------

/* Tcp related function and method */

func NewTcp() *Tcp {
	//var tcp libuv.Tcp
	tcp := libuv.Tcp{}
	return &Tcp{Tcp: tcp}
}

// InitTcp initializes the tcp handle.
func InitTcp(loop *Loop, tcp *Tcp) int {
	return int(libuv.InitTcp(loop.Loop, &tcp.Tcp))
}

// InitTcpEx initializes the tcp handle.
func InitTcpEx(loop *Loop, tcp *Tcp, flags uint) int {
	return int(libuv.InitTcpEx(loop.Loop, &tcp.Tcp, c.Uint(flags)))
}

// Open opens the tcp handle.
func (tcp *Tcp) Open(sock libuv.OsSock) int {
	return int((&tcp.Tcp).Open(sock))
}

// Nodelay sets the nodelay status of the tcp handle.
func (tcp *Tcp) Nodelay(enable int) int {
	return int((&tcp.Tcp).Nodelay(c.Int(enable)))
}

// Keepalive sets the keepalive status of the tcp handle.
func (tcp *Tcp) Keepalive(enable int, delay uint) int {
	return int((&tcp.Tcp).KeepAlive(c.Int(enable), c.Uint(delay)))
}

// SimultaneousAccepts sets the simultaneous accepts status of the tcp handle.
func (tcp *Tcp) SimultaneousAccepts(enable int) int {
	return int((&tcp.Tcp).SimultaneousAccepts(c.Int(enable)))
}

// Bind binds the tcp handle.
func (tcp *Tcp) Bind(addr *net.SockAddr, flags uint) int {
	return int((&tcp.Tcp).Bind(addr, c.Uint(flags)))
}

// Getsockname gets the socket name of the tcp handle.
func (tcp *Tcp) Getsockname(name *net.SockAddr, nameLen *c.Int) int {
	return int((&tcp.Tcp).Getsockname(name, nameLen))
}

// Getpeername gets the peer name of the tcp handle.
func (tcp *Tcp) Getpeername(name *net.SockAddr, nameLen *c.Int) int {
	return int((&tcp.Tcp).Getpeername(name, nameLen))
}

// CloseReset closes the tcp handle with reset.
func (tcp *Tcp) CloseReset(closeCb CloseCb) int {
	tcp.CloseCb = closeCb
	return int((&tcp.Tcp).CloseReset(func(_handle *libuv.Handle) {
		handle := (*Tcp)(unsafe.Pointer(_handle))
		handle.CloseCb((*Handle)(unsafe.Pointer(_handle)))
	}))
}

// TcpConnect connects the tcp handle.
func TcpConnect(req *Connect, tcp *Tcp, addr *net.SockAddr, connectCb ConnectCb) int {
	req.ConnectCb = connectCb
	return int(libuv.TcpConnect(&req.Connect, &tcp.Tcp, addr, func(_req *libuv.Connect, status c.Int) {
		req := (*Connect)(unsafe.Pointer(_req))
		req.ConnectCb(req, status)
	}))
}

// ----------------------------------------------

/* Udp related function and method */

// InitUdp initializes the udp handle.
func InitUdp(loop *Loop, udp *Udp) int {
	return int(libuv.InitUdp(loop.Loop, &udp.Udp))
}

// InitUdpEx initializes the udp handle.
func InitUdpEx(loop *Loop, udp *Udp, flags uint) int {
	return int(libuv.InitUdpEx(loop.Loop, &udp.Udp, c.Uint(flags)))
}

// Open opens the udp handle.
func (udp *Udp) Open(sock libuv.OsSock) int {
	return int((&udp.Udp).Open(sock))
}

// Bind binds the udp handle.
func (udp *Udp) Bind(addr *net.SockAddr, flags uint) int {
	return int((&udp.Udp).Bind(addr, c.Uint(flags)))
}

// Connect connects the udp handle.
func (udp *Udp) Connect(addr *net.SockAddr) int {
	return int((&udp.Udp).Connect(addr))
}

// Getsockname gets the socket name of the udp handle.
func (udp *Udp) Getsockname(name *net.SockAddr, nameLen *c.Int) int {
	return int((&udp.Udp).Getsockname(name, nameLen))
}

// Getpeername gets the peer name of the udp handle.
func (udp *Udp) Getpeername(name *net.SockAddr, nameLen *c.Int) int {
	return int((&udp.Udp).Getpeername(name, nameLen))
}

// SetMembership sets the membership of the udp handle.
func (udp *Udp) SetMembership(multicastAddr string, interfaceAddr string, membership Membership) int {
	return int((&udp.Udp).SetMembership(c.AllocaCStr(multicastAddr), c.AllocaCStr(interfaceAddr), membership.Membership))
}

// SourceMembership sets the source membership of the udp handle.
func (udp *Udp) SourceMembership(multicastAddr string, interfaceAddr string, sourceAddr string, membership Membership) int {
	return int((&udp.Udp).SourceMembership(c.AllocaCStr(multicastAddr), c.AllocaCStr(interfaceAddr), c.AllocaCStr(sourceAddr), membership.Membership))
}

// SetMulticastLoop sets the multicast loop of the udp handle.
func (udp *Udp) SetMulticastLoop(on int) int {
	return int((&udp.Udp).SetMulticastLoop(c.Int(on)))
}

// SetMulticastTTL sets the multicast ttl of the udp handle.
func (udp *Udp) SetMulticastTTL(ttl int) int {
	return int((&udp.Udp).SetMulticastTTL(c.Int(ttl)))
}

// SetMulticastInterface sets the multicast interface of the udp handle.
func (udp *Udp) SetMulticastInterface(interfaceAddr string) int {
	return int((&udp.Udp).SetMulticastInterface(c.AllocaCStr(interfaceAddr)))
}

// SetBroadcast sets the broadcast of the udp handle.
func (udp *Udp) SetBroadcast(on int) int {
	return int((&udp.Udp).SetBroadcast(c.Int(on)))
}

// SetTTL sets the ttl of the udp handle.
func (udp *Udp) SetTTL(ttl int) int {
	return int((&udp.Udp).SetTTL(c.Int(ttl)))
}

// Send sends data to the udp handle.
func Send(req *UdpSend, udp *Udp, bufs *Buf, nbufs uint, addr *net.SockAddr, sendCb UdpSendCb) int {
	req.UdpSendCb = sendCb
	return int(libuv.Send(&req.UdpSend, &udp.Udp, bufs.Buf, c.Uint(nbufs), addr, func(_req *libuv.UdpSend, status c.Int) {
		req := (*UdpSend)(unsafe.Pointer(_req))
		req.UdpSendCb(req, status)
	}))
}

// TrySend tries to send data to the udp handle.
func (udp *Udp) TrySend(bufs *Buf, nbufs uint, addr *net.SockAddr) int {
	return int((&udp.Udp).TrySend(bufs.Buf, c.Uint(nbufs), addr))
}

// StartRecv starts receiving data from the udp handle.
func (udp *Udp) StartRecv(allocCb AllocCb, recvCb UdpRecvCb) int {
	udp.UdpRecvCb = recvCb
	udp.AllocCb = allocCb
	return int((&udp.Udp).StartRecv(func(_handle *libuv.Handle, suggestedSize uintptr, _buf *libuv.Buf) {
		udp := (*Udp)(unsafe.Pointer(_handle))
		buf := &Buf{Buf: _buf}
		udp.AllocCb((*Handle)(unsafe.Pointer(_handle)), suggestedSize, buf)
	}, func(_handle *libuv.Udp, nread c.Long, _buf *libuv.Buf, addr *net.SockAddr, flags c.Uint) {
		handle := (*Udp)(unsafe.Pointer(_handle))
		buf := &Buf{Buf: _buf}
		handle.UdpRecvCb(handle, nread, buf, addr, flags)
	}))
}

// StopRecv stops receiving data from the udp handle.
func (udp *Udp) StopRecv() int {
	return int((&udp.Udp).StopRecv())
}

// UsingRecvmmsg returns true if the udp handle is using recvmmsg.
func (udp *Udp) UsingRecvmmsg() int {
	return int((&udp.Udp).UsingRecvmmsg())
}

// GetSendQueueSize returns the size of the send queue.
func (udp *Udp) GetSendQueueSize() uintptr {
	return (&udp.Udp).GetSendQueueSize()
}

// GetSendQueueCount returns the count of the send queue.
func (udp *Udp) GetSendQueueCount() uintptr {
	return (&udp.Udp).GetSendQueueCount()
}

// ----------------------------------------------

// Ip4Addr returns the ipv4 address.
func Ip4Addr(ip string, port int, addr *net.SockaddrIn) int {
	return int(libuv.Ip4Addr(c.AllocaCStr(ip), c.Int(port), addr))
}

// Ip6Addr returns the ipv6 address.
func Ip6Addr(ip string, port int, addr *net.SockaddrIn6) int {
	return int(libuv.Ip6Addr(c.AllocaCStr(ip), c.Int(port), addr))
}

// Ip4Name returns the ipv4 name.
func Ip4Name(addr *net.SockaddrIn, dst string, size uintptr) int {
	return int(libuv.Ip4Name(addr, c.AllocaCStr(dst), size))
}

// Ip6Name returns the ipv6 name.
func Ip6Name(addr *net.SockaddrIn6, dst string, size uintptr) int {
	return int(libuv.Ip6Name(addr, c.AllocaCStr(dst), size))
}

// IpName returns the ip name.
func IpName(addr *net.SockAddr, dst string, size uintptr) int {
	return int(libuv.IpName(addr, c.AllocaCStr(dst), size))
}

// InetNtop converts the network address to presentation format.
func InetNtop(af int, src unsafe.Pointer, dst string, size uintptr) int {
	return int(libuv.InetNtop(c.Int(af), src, c.AllocaCStr(dst), size))
}

// InetPton converts the presentation format to network address.
func InetPton(af int, src string, dst unsafe.Pointer) int {
	return int(libuv.InetPton(c.Int(af), c.AllocaCStr(src), dst))
}

// ----------------------------------------------

/* Getaddrinfo related function and method */

// Getaddrinfo gets the address information.
func Getaddrinfo(loop *Loop, req *GetAddrInfo, getaddrinfoCb GetaddrinfoCb, node string, service string, hints *net.AddrInfo) int {
	req.GetaddrinfoCb = getaddrinfoCb
	return int(libuv.Getaddrinfo(loop.Loop, &req.GetAddrInfo, func(_req *libuv.GetAddrInfo, status c.Int, res *net.AddrInfo) {
		req := (*GetAddrInfo)(unsafe.Pointer(_req))
		req.GetaddrinfoCb(req, status, res)
	}, c.AllocaCStr(node), c.AllocaCStr(service), hints))
}

// Freeaddrinfo frees the address information.
func Freeaddrinfo(addrInfo *net.AddrInfo) {
	libuv.Freeaddrinfo(addrInfo)
}

// ----------------------------------------------

/* GetNameInfo related function and method */

// Getnameinfo gets the name information.
func Getnameinfo(loop *Loop, req *GetNameInfo, getnameinfoCb GetnameinfoCb, addr *net.SockAddr, flags int) int {
	req.GetnameinfoCb = getnameinfoCb
	return int(libuv.Getnameinfo(loop.Loop, &req.GetNameInfo, func(_req *libuv.GetNameInfo, status c.Int, hostname *c.Char, service *c.Char) {
		req := (*GetNameInfo)(unsafe.Pointer(_req))
		req.GetnameinfoCb(req, status, hostname, service)
	}, addr, c.Int(flags)))
}
