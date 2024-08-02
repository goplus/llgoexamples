package httpget

import (
	"fmt"
	"io"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgoexamples/rust/hyper"
)

type Request struct {
	Method string
	Url    string
}

func NewRequest(method, url string, body io.Reader) (*Request, error) {
	return &Request{
		Method: method,
		Url:    url,
	}, nil
}

func NewHyperRequest(request *Request) (*hyper.Request, error) {
	host, _, uri := parseURL(request.Url)
	method := request.Method
	// Prepare the request
	req := hyper.NewRequest()
	// Set the request method and uri
	if req.SetMethod((*uint8)(&[]byte(method)[0]), c.Strlen(c.AllocaCStr(method))) != hyper.OK {
		return nil, fmt.Errorf("error setting method %s\n", method)
	}
	if req.SetURI((*uint8)(&[]byte(uri)[0]), c.Strlen(c.AllocaCStr(uri))) != hyper.OK {
		return nil, fmt.Errorf("error setting uri %s\n", uri)
	}

	// Set the request headers
	reqHeaders := req.Headers()
	if reqHeaders.Set((*uint8)(&[]byte("Host")[0]), c.Strlen(c.Str("Host")), (*uint8)(&[]byte(host)[0]), c.Strlen(c.AllocaCStr(host))) != hyper.OK {
		return nil, fmt.Errorf("error setting headers\n")
	}
	return req, nil
}
