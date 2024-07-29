package http

import (
	"github.com/goplus/llgo/c"
	"github.com/goplus/llgoexamples/rust/hyper"
)

// SetUserData Set the user data for the task
func SetUserData(task *hyper.Task, userData hyper.ExampleId) {
	var data = userData
	task.SetUserdata(c.Pointer(uintptr(data)))
}
