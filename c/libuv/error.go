package libuv

import (
	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/libuv"
)

type Errno struct {
	libuv.Errno
}

func (e Errno) Error() string {
	return c.GoString(libuv.Strerror(e.Errno))
}

func (e Errno) Name() string {
	return c.GoString(libuv.ErrName(e.Errno))
}

func (e Errno) TranslateSysError() int {
	return int(libuv.TranslateSysError(c.Int(e.Errno)))
}

func Strerror(err int) string {
	return c.GoString(libuv.Strerror(libuv.Errno(err)))
}
