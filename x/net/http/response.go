package http

import (
	"fmt"
	"unsafe"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/libuv"
	"github.com/goplus/llgo/c/os"
	"github.com/goplus/llgo/rust/hyper"
)

type response struct {
	header       Header
	statusCode   int
	written      bool
	body         []byte
	hyperChannel *hyper.ResponseChannel
	hyperResp    *hyper.Response
	request      *Request
	asyncHandler *libuv.Async
}

type body struct {
	data    []byte
	len     uintptr
	readLen uintptr
}

type taskData struct {
	hyperBody   *hyper.Body
	body        *body
	conn        *conn
	hyperTaskID hyperTaskID
}

type hyperTaskID int

const (
	taskSetBody hyperTaskID = iota
	taskGetBody
)

var DefaultChunkSize uintptr = 8192

func newResponse(request *Request, hyperChannel *hyper.ResponseChannel) *response {
	fmt.Printf("[debug] newResponse called\n")

	return &response{
		header:       make(Header),
		hyperChannel: hyperChannel,
		//request:      request,
		statusCode:   200,
		written:      false,
		body:         nil,
		hyperResp:    hyper.NewResponse(),
	}
}

func (r *response) Header() Header {
	return r.header
}

func (r *response) Write(data []byte) (int, error) {
	if !r.written {
		r.WriteHeader(200)
	}
	fmt.Printf("[debug] data: %s\n", string(data))
	r.body = append(r.body, data...)
	fmt.Printf("[debug] r.body: %s\n", string(r.body))
	return len(data), nil
}

func (r *response) WriteHeader(statusCode int) {
	fmt.Println("[debug] WriteHeader called")
	if r.written {
		return
	}
	r.written = true
	r.statusCode = statusCode

	r.hyperResp.SetStatus(uint16(statusCode))

	fmt.Println("[debug] WriteHeaderStatusCode done")

	//debug
	// fmt.Printf("[debug] < HTTP/1.1 %d\n", statusCode)
	// for key, values := range r.header {
	// 	for _, value := range values {
	// 		fmt.Printf("< %s: %s\n", key, value)
	// 	}
	// }

	headers := r.hyperResp.Headers()
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

	fmt.Println("[debug] WriteHeaderHeaders done")

	fmt.Println("[debug] WriteHeader done")
}

func (r *response) finalize() error {
	fmt.Printf("[debug] finalize called\n")
	// err := r.request.Body.Close()
	// if err != nil {
	// 	return err
	// }
	// fmt.Printf("[debug] request body closed\n")

	if !r.written {
		r.WriteHeader(200)
	}

	r.hyperResp = hyper.NewResponse()

	if r.hyperResp == nil {
		return fmt.Errorf("failed to create response")
	}

	bodyData := body{
		data:    r.body,
		len:     uintptr(len(r.body)),
		readLen: 0,
	}
	fmt.Println("[debug] bodyData constructed")

	body := hyper.NewBody()
	if body == nil {
		return fmt.Errorf("failed to create body")
	}
	taskData := &taskData{
		hyperBody:   nil,
		body:        &bodyData,
		conn:        nil,
		hyperTaskID: taskSetBody,
	}
	body.SetDataFunc(setBodyDataFunc)
	body.SetUserdata(unsafe.Pointer(taskData), nil)
	fmt.Println("[debug] bodyData userdata set")

	fmt.Println("[debug] bodyData set")

	resBody := r.hyperResp.SetBody(body)
	if resBody != hyper.OK {
		return fmt.Errorf("failed to set body")
	}
	fmt.Println("[debug] body set")

	r.hyperChannel.Send(r.hyperResp)
	fmt.Println("[debug] response sent")
	return nil
}

func setBodyDataFunc(userdata c.Pointer, ctx *hyper.Context, chunk **hyper.Buf) c.Int {
	fmt.Println("[debug] setBodyDataFunc called")
	taskData := (*taskData)(userdata)
	if taskData == nil {
		fmt.Println("[debug] taskData is nil")
		return hyper.PollError
	}
	body := taskData.body

	if body.len > 0 {
		//debug
		fmt.Println("[debug]<")
		fmt.Printf("[debug]%s\n", string(body.data))

		if body.len > DefaultChunkSize {
			*chunk = hyper.CopyBuf(&body.data[body.readLen], DefaultChunkSize)
			body.readLen += DefaultChunkSize
			body.len -= DefaultChunkSize
		} else {
			*chunk = hyper.CopyBuf(&body.data[body.readLen], body.len)
			body.readLen += body.len
			body.len = 0
		}
		fmt.Println("[debug] setBodyDataFunc done")
		return hyper.PollReady
	}
	if body.len == 0 {
		*chunk = nil
		fmt.Println("[debug] setBodyDataFunc done")
		return hyper.PollReady
	}

	fmt.Printf("[debug] error setting body data: %s\n", c.GoString(c.Strerror(os.Errno)))
	return hyper.PollError
}
