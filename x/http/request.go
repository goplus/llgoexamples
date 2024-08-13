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
	timeout time.Duration
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
	request := &Request{
		Method:  method,
		URL:     parseURL,
		Req:     req,
		Host:    parseURL.Hostname(),
		Header:  make(Header),
		timeout: 0,
	}
	//request.Header.Set("Host", request.Host)
	request.Header["Host"] = []string{request.Host}
	return request, nil
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
		return nil, fmt.Errorf("error setting header: Host: %s\n", host)
	}

	return req, nil
}

// setHeaders sets the headers of the request
func (req *Request) setHeaders() error {
	headers := req.Req.Headers()
	for key, values := range req.Header {
		valueLen := len(values)
		if valueLen > 1 {
			for _, value := range values {
				if headers.Add((*uint8)(&[]byte(key)[0]), c.Strlen(c.AllocaCStr(key)), (*uint8)(&[]byte(value)[0]), c.Strlen(c.AllocaCStr(value))) != hyper.OK {
					return fmt.Errorf("error adding header %s: %s\n", key, value)
				}
			}
		} else if valueLen == 1 {
			if headers.Set((*uint8)(&[]byte(key)[0]), c.Strlen(c.AllocaCStr(key)), (*uint8)(&[]byte(values[0])[0]), c.Strlen(c.AllocaCStr(values[0]))) != hyper.OK {
				return fmt.Errorf("error setting header %s: %s\n", key, values[0])
			}
		} else {
			return fmt.Errorf("error setting header %s: empty value\n", key)
		}
	}
	return nil
}
