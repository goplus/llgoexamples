package http

import (
	"github.com/goplus/llgo/c"
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
	resp.SetStatus(uint16(statusCode))

	headers := resp.Headers()
	for k, v := range r.header {
		for _, val := range v {
			headers.Set(&[]byte(k)[0], uintptr(len(k)), &[]byte(val)[0], uintptr(len(val)))
		}
	}

	r.channel.Send(resp)
}

func (r *Response) finalize() error {
	if !r.written {
		r.WriteHeader(200)
	}

	body := hyper.NewBody()
	//TODO(hackerchai): implement body data func
	body.SetDataFunc()

	resp := hyper.NewResponse()
	resp.SetBody(body)

	r.channel.Send(resp)
	return nil
}

//TODO(hackerchai): implement body chunk reader
func setBodyDataFunc(userdata c.Pointer, ctx *hyper.Context, chunk **hyper.Buf) c.Int {
}