package http

import (
	"errors"
	"fmt"
	"io"
	"sync"
)

type onceError struct {
	sync.Mutex
	err error
}

func (a *onceError) Store(err error) {
	a.Lock()
	defer a.Unlock()
	if a.err != nil {
		return
	}
	a.err = err
}

func (a *onceError) Load() error {
	a.Lock()
	defer a.Unlock()
	return a.err
}

type requestBody struct {
	chunk   []byte
	readCh  chan []byte
	readyCh chan struct{}

	once sync.Once
	done chan struct{}

	rerr onceError
}

var (
	ErrClosedRequestBody = errors.New("request body: read/write on closed body")
)

func newRequestBody() *requestBody {
	return &requestBody{
		readCh:  make(chan []byte, 1),
		readyCh: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
}

func (rb *requestBody) Read(p []byte) (n int, err error) {
	fmt.Println("RequestBody Read called")
	select {
	case <-rb.done:
		fmt.Println("Read done")
		return 0, rb.rerr.Load()
	default:
	}

	if len(rb.chunk) > 0 {
		fmt.Println("Read remaining chunk")
		n = copy(p, rb.chunk)
		rb.chunk = rb.chunk[n:]
		if len(rb.chunk) > 0 {
			return
		}
	}

	fmt.Println("readyCh waiting")
	select {
    case rb.readyCh <- struct{}{}:
        fmt.Println("readyCh signaled")
    default:
        fmt.Println("readyCh skipped (channel full)")
    }

	select {
    case rb.chunk = <-rb.readCh:
        fmt.Printf("Read chunk received: %s\n", string(rb.chunk))
    case <-rb.done:
        return 0, rb.rerr.Load()
	default:
		if len(rb.chunk) == 0 {
			fmt.Println("Read ended")
			return 0, io.EOF
		}
	}
	fmt.Printf("Read chunk received: %s\n", string(rb.chunk))
	if len(rb.chunk) == 0 {
		fmt.Println("Read ended")
		return 0, io.EOF
	}
	n = copy(p, rb.chunk)
	rb.chunk = rb.chunk[n:]
	fmt.Printf("Read chunk copied: %d bytes\n", n)
	return
}

func (rb *requestBody) readCloseError() error {
	if rerr := rb.rerr.Load(); rerr != nil {
		return rerr
	}
	return ErrClosedRequestBody
}

func (rb *requestBody) closeRead(err error) error {
	fmt.Println("closeRead called")
	if err == nil {
		err = ErrClosedRequestBody
	}
	rb.rerr.Store(err)
	rb.once.Do(func() {
		close(rb.done)
	})
	return nil
}

func (rb *requestBody) Close() error {
	return rb.closeRead(nil)
}
