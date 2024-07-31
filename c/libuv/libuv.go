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

type Loop struct {
	*libuv.Loop
}

type Handle struct {
	*libuv.Handle
}

type Stream struct {
	*libuv.Stream
}

type Poll struct {
	*libuv.Poll
}

type Req struct {
	*libuv.Req
}

type GetAddrInfo struct {
	*libuv.GetAddrInfo
}

type GetNameInfo struct {
	*libuv.GetNameInfo
}

type Shutdown struct {
	*libuv.Shutdown
}

type Write struct {
	*libuv.Write
}

type Connect struct {
	*libuv.Connect
}

type Buf struct {
	*libuv.Buf
}

type WalkCb func(handle *Handle, arg c.Pointer)

func convertWalkCb(callback WalkCb) func(handle *libuv.Handle, arg c.Pointer) {
	return func(handle *libuv.Handle, arg c.Pointer) {
		hand := &Handle{Handle: handle}
		callback(hand, arg)
	}
}

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
	return int(libuv.LoopInit(l.Loop))
}

// Run runs the loop.
func (l *Loop) Run(mode libuv.RunMode) int {
	return int(libuv.Run(l.Loop, mode))
}

// Stop closes the loop.
func (l *Loop) Stop() int {
	return int(libuv.LoopClose(l.Loop))
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
	return int(libuv.LoopDelete(l.Loop))
}

// Alive returns the status of the loop.
func (l *Loop) Alive() int {
	return int(libuv.LoopAlive(l.Loop))
}

// Close closes the loop.
func (l *Loop) Close() int {
	return int(libuv.LoopClose(l.Loop))
}

// Configure configures the loop.
func (l *Loop) Configure(loop *Loop, option libuv.LoopOption, arg int) int {
	return int(libuv.LoopConfigure(l.Loop, option, c.Int(arg)))
}

// Walk walks the loop.
func (l *Loop) Walk(walkCb WalkCb, arg c.Pointer) {
	libuv.LoopWalk(l.Loop, convertWalkCb(walkCb), arg)
}

// Fork forks the loop.
func (l *Loop) Fork(loop *Loop) int {
	return int(libuv.LoopFork(l.Loop))
}

// UpdateTime updates the time of the loop.
func (l *Loop) UpdateTime() {
	libuv.LoopUpdateTime(l.Loop)
}

// Now returns the current time of the loop.
func (l *Loop) Now() uint64 {
	return uint64(libuv.LoopNow(l.Loop))
}

// BackendFd returns the backend file descriptor of the loop.
func (l *Loop) BackendFd() int {
	return int(libuv.LoopBackendFd(l.Loop))
}

// BackendTimeout returns the backend timeout of the loop.
func (l *Loop) BackendTimeout() int {
	return int(libuv.LoopBackendTimeout(l.Loop))
}

// ----------------------------------------------

/* Buf related functions and method. */

// InitBuf initializes a buffer with the given c.Char slice.
func InitBuf(buffer []c.Char) Buf {
	buf := libuv.InitBuf((*c.Char)(unsafe.Pointer(&buffer[0])), c.Uint(unsafe.Sizeof(buffer)))
	return Buf{Buf: &buf}
}
