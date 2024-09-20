package http

import (
	"errors"

	"github.com/goplus/llgo/c/libuv"
)

type bodyStream struct {
	chunk       []byte
	readCh      chan []byte
	asyncHandle *libuv.Async

	done chan struct{}

	rerr error
}

var (
	ErrClosedBodyStream = errors.New("body stream: read/write on closed body")
)

func newBodyStream(asyncHandle *libuv.Async) *bodyStream {
	return &bodyStream{
		readCh:      make(chan []byte, 1),
		done:        make(chan struct{}),
		asyncHandle: asyncHandle,
	}
}

func (rb *bodyStream) Read(p []byte) (n int, err error) {
	select {
	case <-rb.done:
		err = rb.readCloseError()
		return
	default:
	}

	for n < len(p) {
		if len(rb.chunk) == 0 {
			rb.asyncHandle.Send()
			select {
			case chunk := <-rb.readCh:
				rb.chunk = chunk
			case <-rb.done:
				err = rb.readCloseError()
				return
			}
		}

		copied := copy(p[n:], rb.chunk)
		n += copied
		rb.chunk = rb.chunk[copied:]
	}

	return
}

func (rb *bodyStream) readCloseError() error {
	if rerr := rb.rerr; rerr != nil {
		return rerr
	}
	return ErrClosedBodyStream
}

func (rb *bodyStream) closeWithError(err error) error {
	if rb.rerr != nil {
		return nil
	}
	if err == nil {
		err = ErrClosedBodyStream
	}
	rb.rerr = err
	close(rb.done)
	return nil
}

func (rb *bodyStream) Close() error {
	return rb.closeWithError(nil)
}
