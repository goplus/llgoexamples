package http

import (
	"errors"
	"fmt"
	"io"

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
	fmt.Println("[debug] requestBody.Read called")
	// If there are still unread chunks, read them first
	if len(rb.chunk) > 0 {
		n = copy(p, rb.chunk)
		rb.chunk = rb.chunk[n:]
		return n, nil
	}

	// Attempt to read a new chunk from a channel
	select {
	case chunk, ok := <-rb.readCh:
		if !ok {
			// The channel has been closed, indicating that all data has been read
			return 0, rb.readCloseError()
		}
		n = copy(p, chunk)
		if n < len(chunk) {
			// If the capacity of p is insufficient to hold the whole chunk, save the rest of the chunk
			rb.chunk = chunk[n:]
		}
		fmt.Println("[debug] requestBody.Read async send")
		rb.asyncHandle.Send()
		return n, nil
	case <-rb.done:
		// If the done channel is closed, the read needs to be terminated
		return 0, rb.readCloseError()
	}
}

func (rb *requestBody) readCloseError() error {
	if rerr := rb.rerr; rerr != nil {
		return rerr
	}
	return ErrClosedRequestBody
}

func (rb *requestBody) closeRead(err error) error {
	fmt.Println("[debug] RequestBody closeRead called")
	if err == nil {
		err = io.EOF
	}
	rb.rerr = err
	close(rb.done)
	return nil
}

func (rb *requestBody) Close() error {
	return rb.closeRead(nil)
}
