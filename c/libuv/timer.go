package libuv

import (
	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/libuv"
)

type Timer struct {
	*libuv.Timer
	TimerCb func(timer *Timer)
}

// ----------------------------------------------

type TimerCb func(timer *Timer)

// ----------------------------------------------

/* Timer related function and method */

// InitTimer initializes the timer.
func InitTimer(loop *Loop, timer *Timer) int {
	return int(libuv.InitTimer(loop.Loop, timer.Timer))
}

// Start starts the timer.
func (timer *Timer) Start(cb TimerCb, timeout uint64, repeat uint64) int {
	timer.TimerCb = cb
	return int(timer.Timer.Start(func(_timer *libuv.Timer) {
		timer := (*Timer)(c.Pointer(_timer))
		timer.TimerCb(timer)
	}, timeout, repeat))
}

// Stop stops the timer.
func (timer *Timer) Stop() int {
	return int(timer.Timer.Stop())
}

// Again agains the timer.
func (timer *Timer) Again() int {
	return int(timer.Timer.Again())
}

// SetRepeat sets the repeat of the timer.
func (timer *Timer) SetRepeat(repeat uint64) {
	timer.Timer.SetRepeat(repeat)
}

// GetRepeat gets the repeat of the timer.
func (timer *Timer) GetRepeat() uint64 {
	return timer.Timer.GetRepeat()
}

// GetDueIn gets the due in of the timer.
func (timer *Timer) GetDueIn() uint64 {
	return timer.Timer.GetDueIn()
}
