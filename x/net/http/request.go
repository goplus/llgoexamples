package http

import (
	"fmt"
	"io"
	"unsafe"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/rust/hyper"
	cos "github.com/goplus/llgo/c/os"
)

type Request struct {
	Conn *Conn
	Method string
	URL    string
	Header Header
	Body   io.ReadCloser
}

func newRequest(conn *Conn, hyperReq *hyper.Request) (*Request, error) {
	method := make([]byte, 32)
	methodLen := uintptr(len(method))
	if err := hyperReq.Method(&method[0], &methodLen); err != hyper.OK {
		return nil, fmt.Errorf("failed to get method: %v", err)
	}

	methodStr := string(method[:methodLen])

	var scheme, authority, pathAndQuery [1024]byte
	schemeLen, authorityLen, pathAndQueryLen := uintptr(len(scheme)), uintptr(len(authority)), uintptr(len(pathAndQuery))
	if err := hyperReq.URIParts(&scheme[0], &schemeLen, &authority[0], &authorityLen, &pathAndQuery[0], &pathAndQueryLen); err != hyper.OK {
		return nil, fmt.Errorf("failed to get URI parts: %v", err)
	}

	req := &Request{
		Conn: conn,
		Method: methodStr,
		URL:    fmt.Sprintf("%s://%s%s", string(scheme[:schemeLen]), string(authority[:authorityLen]), string(pathAndQuery[:pathAndQueryLen])),
		Header: make(Header),
	}

	headers := hyperReq.Headers()
	if headers != nil {
		headers.Foreach(addHeader, c.Pointer(req))
	} else {
		return nil, fmt.Errorf("failed to get request headers")
	}

	if methodStr == "POST" || methodStr == "PUT" {
		body := hyperReq.Body()
		if body != nil {
			// task := body.Foreach(getBodyChunk, c.Pointer(req), nil)
			// if task != nil {
			// 	r := conn.Executor.Push(task)
			// 	if r != hyper.OK {
			// 		task.Free()
			// 		return nil, fmt.Errorf("failed to push body foreach task: %v", r)
			// 	}
			// } else {
			// 	return nil, fmt.Errorf("failed to create body foreach task")
			// }

		} else {
			return nil, fmt.Errorf("failed to get request body")
		}
	}


	return req, nil
}

func addHeader(data unsafe.Pointer, name *byte, nameLen uintptr, value *byte, valueLen uintptr) c.Int {
	req := (*Request)(data)
	key := string(unsafe.Slice(name, nameLen))
	val := string(unsafe.Slice(value, valueLen))
	req.Header.Add(key, val)
	return hyper.IterContinue
}

//TODO(hackerchai): implement body chunk reader
func getBodyChunk(userdata c.Pointer, chunk *hyper.Buf) c.Int {
	req := (*Request)(userdata)
	buf := chunk.Bytes()
	len := chunk.Len()
	cos.Write(1, unsafe.Pointer(buf), len)

	return hyper.IterContinue
}