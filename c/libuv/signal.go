package libuv

import (
	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/libuv"
)

type Signal struct {
	libuv.Signal
	SignalCb func(handle *Signal, sigNum c.Int)
}

// ----------------------------------------------

/* Function type */

type SignalCb func(handle *Signal, sigNum c.Int)

// ----------------------------------------------

/* Signal related functions and method. */

// SignalInit initializes the signal handle with the given loop
func SignalInit(loop *Loop, handle *Signal) int {
	return int(libuv.SignalInit(loop.Loop, &handle.Signal))
}

// Start starts the signal handle with the given callback and signal number
func (signal *Signal) Start(cb SignalCb, signum int) int {
	signal.SignalCb = cb
	return int((&signal.Signal).Start(func(_handle *libuv.Signal, sigNum c.Int) {
		handle := (*Signal)(c.Pointer(_handle))
		handle.SignalCb(handle, sigNum)
	}, c.Int(signum)))
}

// StartOneshot starts the signal handle with the given callback and signal number
func (signal *Signal) StartOneshot(cb SignalCb, signum int) int {
	signal.SignalCb = cb
	return int((&signal.Signal).StartOneshot(func(_handle *libuv.Signal, sigNum c.Int) {
		handle := (*Signal)(c.Pointer(_handle))
		handle.SignalCb(handle, sigNum)
	}, c.Int(signum)))
}
