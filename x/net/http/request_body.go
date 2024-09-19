package http

import (
	"errors"
	"fmt"

	"github.com/goplus/llgo/c/libuv"
)

type requestBody struct {
	chunk       []byte
	readCh      chan []byte
	asyncHandle *libuv.Async

	done chan struct{}

	rerr error
}

var (
	ErrClosedRequestBody = errors.New("request body: read/write on closed body")
)

func newRequestBody(asyncHandle *libuv.Async) *requestBody {
	return &requestBody{
		readCh:      make(chan []byte, 1),
		done:        make(chan struct{}),
		asyncHandle: asyncHandle,
	}
}

func (rb *requestBody) Read(p []byte) (n int, err error) {
	fmt.Println("[debug] RequestBody Read called")
	select {
	case <-rb.done:
		err = rb.readCloseError()
		return
	default:
	}

	for n < len(p) {
		if len(rb.chunk) == 0 {
			rb.asyncHandle.Send()
			fmt.Println("[debug] RequestBody Read asyncHandle.Send called")
			select {
			case chunk := <-rb.readCh:
				rb.chunk = chunk
				fmt.Println("[debug] RequestBody Read chunk received")
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

func (rb *requestBody) readCloseError() error {
	if rerr := rb.rerr; rerr != nil {
		return rerr
	}
	return ErrClosedRequestBody
}

func (rb *requestBody) closeRead(err error) error {
	fmt.Println("[debug] RequestBody closeRead called")
	if rb.rerr != nil {
		return nil
	}
	if err == nil {
		err = ErrClosedRequestBody
	}
	rb.rerr = err
	close(rb.done)
	return nil
}

func (rb *requestBody) Close() error {
	return rb.closeRead(nil)
}
