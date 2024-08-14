package http

import (
	"fmt"
	"io"
	"unsafe"

	"github.com/goplus/llgo/rust/hyper"
)

type Request struct {
	Method string
	URL    string
	Header Header
	Body   io.ReadCloser
}

func newRequest(hyperReq *hyper.Request) (*Request, error) {
	method := make([]byte, 32)
	methodLen := uintptr(len(method))
	if err := hyperReq.Method(&method[0], &methodLen); err != hyper.OK {
		return nil, fmt.Errorf("failed to get method: %v", err)
	}

	var scheme, authority, pathAndQuery [1024]byte
	schemeLen, authorityLen, pathAndQueryLen := uintptr(len(scheme)), uintptr(len(authority)), uintptr(len(pathAndQuery))
	if err := hyperReq.URIParts(&scheme[0], &schemeLen, &authority[0], &authorityLen, &pathAndQuery[0], &pathAndQueryLen); err != hyper.OK {
		return nil, fmt.Errorf("failed to get URI parts: %v", err)
	}

	req := &Request{
		Method: string(method[:methodLen]),
		URL:    fmt.Sprintf("%s://%s%s", string(scheme[:schemeLen]), string(authority[:authorityLen]), string(pathAndQuery[:pathAndQueryLen])),
		Header: make(Header),
	}

	headers := hyperReq.Headers()
	if headers != nil {
		headers.Foreach(func(name *byte, nameLen uintptr, value *byte, valueLen uintptr) int {
			key := string(unsafe.Slice(name, nameLen))
			val := string(unsafe.Slice(value, valueLen))
			req.Header.Add(key, val)
			return hyper.IterContinue
		}, nil)
	}

	return req, nil
}