package http

import (
	"fmt"
	"unsafe"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/os"
	"github.com/goplus/llgo/rust/hyper"
)

type response struct {
	header     Header
	statusCode int
	written    bool
	body       []byte
	channel    *hyper.ResponseChannel
}

type body struct {
	data    []byte
	len     uintptr
	readLen uintptr
}

var DefaultChunkSize uintptr = 8192


func newResponse(channel *hyper.ResponseChannel) *response {
	return &response{
		header:  make(Header),
		channel: channel,
	}
}

func (r *response) Header() Header {
	return r.header
}

func (r *response) Write(data []byte) (int, error) {
	if !r.written {
		r.WriteHeader(200)
	}
	r.body = append(r.body, data...)
	return len(data), nil
}

func (r *response) WriteHeader(statusCode int) {
	if r.written {
		return
	}
	r.written = true
	r.statusCode = statusCode

	resp := hyper.NewResponse()
	resp.SetStatus(uint16(statusCode))

	headers := resp.Headers()
	for key, values := range r.header {
		valueLen := len(values)
		if valueLen > 1 {
			for _, value := range values {
				if headers.Add(&[]byte(key)[0], c.Strlen(c.AllocaCStr(key)), &[]byte(value)[0], c.Strlen(c.AllocaCStr(value))) != hyper.OK {
					return 
				}
			}
		} else if valueLen == 1 {
			if headers.Set(&[]byte(key)[0], c.Strlen(c.AllocaCStr(key)), &[]byte(values[0])[0], c.Strlen(c.AllocaCStr(values[0]))) != hyper.OK {
				return
			}
		} else {
			return
		}
	}

	r.channel.Send(resp)
}

func (r *response) finalize() error {
	if !r.written {
		r.WriteHeader(200)
	}

	bodyData := &body{
		data: r.body,
		len: uintptr(len(r.body)),
		readLen: 0,
	}

	body := hyper.NewBody()
	body.SetUserdata(unsafe.Pointer(bodyData), nil)

	body.SetDataFunc(setBodyDataFunc)

	resp := hyper.NewResponse()
	resp.SetBody(body)

	r.channel.Send(resp)
	return nil
}

func setBodyDataFunc(userdata c.Pointer, ctx *hyper.Context, chunk **hyper.Buf) c.Int {
	body := (*body)(userdata)
	if body.len > 0 {
		if body.len > DefaultChunkSize {
			*chunk = hyper.CopyBuf(&body.data[body.readLen], DefaultChunkSize)
			body.readLen += DefaultChunkSize
			body.len -= DefaultChunkSize
		} else {
			*chunk = hyper.CopyBuf(&body.data[body.readLen], body.len)
			body.readLen += body.len
			body.len = 0
		}
		return hyper.PollReady
	}
	if body.len == 0 {
		*chunk = nil
		return hyper.PollReady
	}

	fmt.Printf("error setting body data: %s\n", c.GoString(c.Strerror(os.Errno)))
	return hyper.PollError
}