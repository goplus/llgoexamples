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
	resp       *hyper.Response
}

type body struct {
	data    []byte
	len     uintptr
	readLen uintptr
}

var DefaultChunkSize uintptr = 8192


func newResponse(channel *hyper.ResponseChannel) *response {
	fmt.Printf("newResponse called\n")
	resp := response{
		header:  make(Header),
		channel: channel,
	}
	return &resp
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
	fmt.Printf("WriteHeader called\n")
	if r.written {
		return
	}
	r.written = true
	r.statusCode = statusCode

	newResp := hyper.NewResponse()

	newResp.SetStatus(uint16(statusCode))

	headers := newResp.Headers()
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
	r.resp = newResp
}

func (r *response) finalize() error {
	fmt.Printf("finalize called\n")
	if !r.written {
		r.WriteHeader(200)
	}

	bodyData := body{
		data: r.body,
		len: uintptr(len(r.body)),
		readLen: 0,
	}
	fmt.Printf("bodyData constructed\n")

	body := hyper.NewBody()
	if body == nil {
		return fmt.Errorf("failed to create body")
	}
	body.SetDataFunc(setBodyDataFunc)
	body.SetUserdata(unsafe.Pointer(&bodyData), nil)
	fmt.Printf("bodyData userdata set\n")

	fmt.Printf("bodyData set\n")

	resBody := r.resp.SetBody(body)
	if resBody != hyper.OK {
		return fmt.Errorf("failed to set body")
	}
	fmt.Printf("body set\n")

	r.channel.Send(r.resp)
	fmt.Printf("response sent\n")
	return nil
}

func setBodyDataFunc(userdata c.Pointer, ctx *hyper.Context, chunk **hyper.Buf) c.Int {
	fmt.Printf("setBodyDataFunc called\n")
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