package http

import (
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgoexamples/rust/hyper"
)

type Request struct {
	Method  string
	URL     *url.URL
	Req     *hyper.Request
	Host    string
	Header  Header
	Timeout time.Duration
}

func NewRequest(method, urlStr string, body io.Reader) (*Request, error) {
	parseURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	req, err := newHyperRequest(method, parseURL)
	if err != nil {
		return nil, err
	}
	return &Request{
		Method: method,
		URL:    parseURL,
		Req:    req,
		Host:   parseURL.Hostname(),
	}, nil
}

func newHyperRequest(method string, URL *url.URL) (*hyper.Request, error) {
	host := URL.Hostname()
	uri := URL.RequestURI()
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
