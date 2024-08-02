package libuv

import (
	"unsafe"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/libuv"
)

const (
	RUN_DEFAULT = libuv.RUN_DEFAULT
	RUN_ONCE    = libuv.RUN_ONCE
	RUN_NOWAIT  = libuv.RUN_NOWAIT
)

const (
	LOOP_BLOCK_SIGNAL = libuv.LOOP_BLOCK_SIGNAL
	METRICS_IDLE_TIME = libuv.METRICS_IDLE_TIME
)

const (
	UV_LEAVE_GROUP = libuv.UV_LEAVE_GROUP
	UV_JOIN_GROUP  = libuv.UV_JOIN_GROUP
)

const (
	UNKNOWN_HANDLE  = libuv.UNKNOWN_HANDLE
	ASYNC           = libuv.ASYNC
	CHECK           = libuv.CHECK
	FS_EVENT        = libuv.FS_EVENT
	FS_POLL         = libuv.FS_POLL
	HANDLE          = libuv.HANDLE
	IDLE            = libuv.IDLE
	NAMED_PIPE      = libuv.NAMED_PIPE
	POLL            = libuv.POLL
	PREPARE         = libuv.PREPARE
	PROCESS         = libuv.PROCESS
	STREAM          = libuv.STREAM
	TCP             = libuv.TCP
	TIMER           = libuv.TIMER
	TTY             = libuv.TTY
	UDP             = libuv.UDP
	SIGNAL          = libuv.SIGNAL
	FILE            = libuv.FILE
	HANDLE_TYPE_MAX = libuv.HANDLE_TYPE_MAX
)

const (
	UNKNOWN_REQ      = libuv.UNKNOWN_REQ
	REQ              = libuv.REQ
	CONNECT          = libuv.CONNECT
	WRITE            = libuv.WRITE
	SHUTDOWN         = libuv.SHUTDOWN
	UDP_SEND         = libuv.UDP_SEND
	FS               = libuv.FS
	WORK             = libuv.WORK
	GETADDRINFO      = libuv.GETADDRINFO
	GETNAMEINFO      = libuv.GETNAMEINFO
	RANDOM           = libuv.RANDOM
	REQ_TYPE_PRIVATE = libuv.REQ_TYPE_PRIVATE
	REQ_TYPE_MAX     = libuv.REQ_TYPE_MAX
)

const (
	READABLE       = libuv.READABLE
	WRITABLE       = libuv.WRITABLE
	DISCONNECT     = libuv.DISCONNECT
	PRIPRIORITIZED = libuv.PRIPRIORITIZED
)

type Loop struct {
	*libuv.Loop
	WalkCb WalkCb
}

type Poll struct {
	*libuv.Poll
	PollCb func(handle *Poll, status c.Int, events c.Int)
}

type Buf struct {
	*libuv.Buf
}

type WalkCb func(handle *Handle, arg c.Pointer)

type PollCb func(handle *Poll, status c.Int, events c.Int)

type MallocFunc func(size uintptr) c.Pointer

type ReallocFunc func(ptr c.Pointer, size uintptr) c.Pointer

type CallocFunc func(count uintptr, size uintptr) c.Pointer

type FreeFunc func(ptr c.Pointer)

// ----------------------------------------------

// Version returns the version of the libuv.
func Version() uint {
	return uint(libuv.Version())
}

// VersionString returns the version string of the libuv.
func VersionString() string {
	return c.GoString(libuv.VersionString())
}

// LibraryShutdown closes the library.
func LibraryShutdown() {
	libuv.LibraryShutdown()
}

// ReplaceAllocator replaces the allocator.
// TODO
//func ReplaceAllocator(mallocFunc libuv.MallocFunc, reallocFunc libuv.ReallocFunc, callocFunc libuv.CallocFunc, freeFunc libuv.FreeFunc) {
//	libuv.ReplaceAllocator(mallocFunc, reallocFunc, callocFunc, freeFunc)
//}

// ----------------------------------------------

// DefaultLoop returns the default loop.
func DefaultLoop() *Loop {
	return &Loop{Loop: libuv.DefaultLoop()}
}

// Size returns the size of the loop.
func (l *Loop) Size() uintptr {
	return libuv.LoopSize()
}

// Init initializes the loop.
func (l *Loop) Init() int {
	return int(l.Loop.Init())
}

// Run runs the loop.
func (l *Loop) Run(mode libuv.RunMode) int {
	return int(l.Loop.Run(mode))
}

// Stop closes the loop.
func (l *Loop) Stop() {
	l.Loop.Stop()
}

// Default creates a new loop.
func (l *Loop) Default() *libuv.Loop {
	return libuv.LoopDefault()
}

// New creates a new loop.
func (l *Loop) New() *libuv.Loop {
	return libuv.LoopNew()
}

// Deprecated: use LoopClose instead.
// Delete closes the loop.
func (l *Loop) Delete() int {
	return int(l.Loop.Delete())
}

// Alive returns the status of the loop.
func (l *Loop) Alive() int {
	return int(l.Loop.Alive())
}

// Close closes the loop.
func (l *Loop) Close() int {
	return int(l.Loop.Close())
}

// Configure configures the loop.
func (l *Loop) Configure(loop *Loop, option libuv.LoopOption, arg int) int {
	return int(l.Loop.Configure(option, c.Int(arg)))
}

// Walk walks the loop.
func (l *Loop) Walk(walkCb WalkCb, arg c.Pointer) {
	l.WalkCb = walkCb
	l.Loop.Walk(func(_handle *libuv.Handle, arg c.Pointer) {
		handle := (*Handle)(unsafe.Pointer(_handle))
		l.WalkCb(handle, arg)
	}, arg)
}

// Fork forks the loop.
func (l *Loop) Fork(loop *Loop) int {
	return int(l.Loop.Fork())
}

// UpdateTime updates the time of the loop.
func (l *Loop) UpdateTime() {
	l.Loop.UpdateTime()
}

// Now returns the current time of the loop.
func (l *Loop) Now() uint64 {
	return uint64(l.Loop.Now())
}

// BackendFd returns the backend file descriptor of the loop.
func (l *Loop) BackendFd() int {
	return int(l.Loop.BackendFd())
}

// BackendTimeout returns the backend timeout of the loop.
func (l *Loop) BackendTimeout() int {
	return int(l.Loop.BackendTimeout())
}

// ----------------------------------------------

/* Buf related functions and method. */

// InitBuf initializes a buffer with the given c.Char slice.
func InitBuf(buffer []c.Char) Buf {
	buf := libuv.InitBuf((*c.Char)(unsafe.Pointer(&buffer[0])), c.Uint(unsafe.Sizeof(buffer)))
	return Buf{Buf: &buf}
}

// ----------------------------------------------

/* Poll related function and method */

// PollInit initializes the poll.
func PollInit(loop *Loop, poll *Poll, fd libuv.OsFd) int {
	return int(libuv.PollInit(loop.Loop, poll.Poll, fd))
}

// PollStart starts the poll.
func PollStart(poll *Poll, events int, cb PollCb) int {
	poll.PollCb = cb
	return int(libuv.PollStart(poll.Poll, c.Int(events), func(_handle *libuv.Poll, status c.Int, events c.Int) {
		handle := (*Poll)(unsafe.Pointer(_handle))
		handle.PollCb(handle, status, events)
	}))
}

// PollStop stops the poll.
func PollStop(poll *Poll) int {
	return int(libuv.PollStop(poll.Poll))
}

// PollInitSocket initializes the poll with the given socket.
func PollInitSocket(loop *Loop, poll *Poll, socket int) int {
	return int(libuv.PollInitSocket(loop.Loop, poll.Poll, c.Int(socket)))
}
