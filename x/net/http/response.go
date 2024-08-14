package http

import (
	"unsafe"

	"github.com/goplus/llgo/rust/hyper"
)

type Response struct {
	header     Header
	statusCode int
	written    bool
	body       []byte
	channel    *hyper.ResponseChannel
}

func newResponse(channel *hyper.ResponseChannel) *Response {
	return &Response{
		header:  make(Header),
		channel: channel,
	}
}

func (r *Response) Header() Header {
	return r.header
}

func (r *Response) Write(data []byte) (int, error) {
	if !r.written {
		r.WriteHeader(200)
	}
	r.body = append(r.body, data...)
	return len(data), nil
}

func (r *Response) WriteHeader(statusCode int) {
	if r.written {
		return
	}
	r.written = true
	r.statusCode = statusCode

	resp := hyper.NewResponse()
	resp.SetStatus(uint(statusCode))

	headers := resp.Headers()
	for k, v := range r.header {
		for _, val := range v {
			headers.Set([]byte(k), uintptr(len(k)), []byte(val), uintptr(len(val)))
		}
	}

	r.channel.Send(resp)
}

func (r *Response) finalize() error {
	if !r.written {
		r.WriteHeader(200)
	}

	body := hyper.NewBody()
	body.SetDataFunc(func(userdata unsafe.Pointer, ctx *hyper.Context, chunk **hyper.Buf) int {
		*chunk = hyper.CopyBuf(r.body, uintptr(len(r.body)))
		r.body = nil // Clear the body after sending
		return hyper.PollReady
	})

	resp := hyper.NewResponse()
	resp.SetBody(body)

	r.channel.Send(resp)
	return nil
}