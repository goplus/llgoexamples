package http

import (
	"errors"

	"github.com/goplus/llgo/c/libuv"
)

type bodyChunk struct {
	chunk       []byte
	readCh      chan []byte
	asyncHandle *libuv.Async

	done chan struct{}

	rerr error
}

var (
	errClosedBodyChunk = errors.New("bodyChunk: read/write on closed body")
)

func newBodyChunk(asyncHandle *libuv.Async) *bodyChunk {
	return &bodyChunk{
		readCh:      make(chan []byte, 1),
		done:        make(chan struct{}),
		asyncHandle: asyncHandle,
	}
}

func (bc *bodyChunk) Read(p []byte) (n int, err error) {
	select {
	case <-bc.done:
		err = bc.readCloseError()
		return
	default:
	}

	for n < len(p) {
		if len(bc.chunk) == 0 {
			bc.asyncHandle.Send()
			select {
			case chunk := <-bc.readCh:
				bc.chunk = chunk
			case <-bc.done:
				err = bc.readCloseError()
				return
			}
		}

		copied := copy(p[n:], bc.chunk)
		n += copied
		bc.chunk = bc.chunk[copied:]
	}

	return
}

func (bc *bodyChunk) Close() error {
	return bc.closeWithError(nil)
}

func (bc *bodyChunk) readCloseError() error {
	if rerr := bc.rerr; rerr != nil {
		return rerr
	}
	return errClosedBodyChunk
}

func (bc *bodyChunk) closeWithError(err error) error {
	if bc.rerr != nil {
		return nil
	}
	if err == nil {
		err = errClosedBodyChunk
	}
	bc.rerr = err
	close(bc.done)
	return nil
}
