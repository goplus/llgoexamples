package http

import (
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
	"unsafe"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/rust/hyper"
)

type Request struct {
	Method           string
	URL              *url.URL
	Proto            string // "HTTP/1.0"
	ProtoMajor       int    // 1
	ProtoMinor       int    // 0
	Header           Header
	Body             io.ReadCloser
	GetBody          func() (io.ReadCloser, error)
	ContentLength    int64
	TransferEncoding []string
	Close            bool
	Host             string
	timeout          time.Duration
}

func newRequest(ListenAddr string, conn *conn, hyperReq *hyper.Request) (*Request, error) {
	method := make([]byte, 32)
	methodLen := unsafe.Sizeof(method)
	if err := hyperReq.Method(&method[0], &methodLen); err != hyper.OK {
		return nil, fmt.Errorf("failed to get method: %v", err)
	}

	methodStr := string(method[:methodLen])
	fmt.Printf("Method: %s\n", methodStr)

	var scheme, authority, pathAndQuery [1024]byte
	schemeLen, authorityLen, pathAndQueryLen := unsafe.Sizeof(scheme), unsafe.Sizeof(authority), unsafe.Sizeof(pathAndQuery)
	uriResult := hyperReq.URIParts(&scheme[0], &schemeLen, &authority[0], &authorityLen, &pathAndQuery[0], &pathAndQueryLen);
	if uriResult != hyper.OK {
		return nil, fmt.Errorf("failed to get URI parts: %v", uriResult)
	}

	var schemeStr, authorityStr, pathAndQueryStr string
	if schemeLen == 0 {
		schemeStr = "http"
	} else {
		schemeStr = string(scheme[:schemeLen])
	}

	if authorityLen == 0 {
		authorityStr = ListenAddr
	} else {
		authorityStr = string(authority[:authorityLen])
	}

	if pathAndQueryLen == 0 {
		return nil, fmt.Errorf("failed to get URI path and query: %v", uriResult)
	} else {
		pathAndQueryStr = string(pathAndQuery[:pathAndQueryLen])
	}


	var proto string
	var protoMajor, protoMinor int
	version := hyperReq.Version()
	fmt.Printf("Version: %d\n", version)
	switch version {
	case hyper.HTTPVersion10:
		proto = "HTTP/1.0"
		protoMajor = 1
		protoMinor = 0
	case hyper.HTTPVersion11:
		proto = "HTTP/1.1"
		protoMajor = 1
		protoMinor = 1
	case hyper.HTTPVersion2:
		proto = "HTTP/2.0"
		protoMajor = 2
		protoMinor = 0
	case hyper.HTTPVersionNone:
		proto = "HTTP/0.0"
		protoMajor = 0
		protoMinor = 0
	default:
		return nil, fmt.Errorf("unknown HTTP version: %d", version)
	}

	urlStr := fmt.Sprintf("%s://%s%s", schemeStr, authorityStr, pathAndQueryStr)
	fmt.Printf("URL: %s\n", urlStr)
	url, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	req := Request{
		Method:     methodStr,
		URL:        url,
		Proto:      proto,
		ProtoMajor: protoMajor,
		ProtoMinor: protoMinor,
		Header:     make(Header),
		Host:       authorityStr,
		timeout:    0,
	}

	headers := hyperReq.Headers()
	if headers != nil {
		headers.Foreach(addHeader, unsafe.Pointer(&req))
	} else {
		return nil, fmt.Errorf("failed to get request headers")
	}

	if methodStr == "POST" || methodStr == "PUT" || methodStr == "PATCH" {
		body := hyperReq.Body()
		if body != nil {
			bodyWriter := new(io.PipeWriter)
			req.Body, bodyWriter = io.Pipe()


			task := body.Foreach(getBodyChunk, c.Pointer(&bodyWriter), freeBodyWriter)
			if task != nil {
				r := conn.Executor.Push(task)
				if r != hyper.OK {
					task.Free()
					return nil, fmt.Errorf("failed to push body foreach task: %v", r)
				}
			} else {
				return nil, fmt.Errorf("failed to create body foreach task")
			}

		} else {
			return nil, fmt.Errorf("failed to get request body")
		}
	}

	return &req, nil
}

func addHeader(data unsafe.Pointer, name *byte, nameLen uintptr, value *byte, valueLen uintptr) c.Int {
	req := (*Request)(data)
	key := string(unsafe.Slice(name, nameLen))
	val := string(unsafe.Slice(value, valueLen))
	values := strings.Split(val, ",")
	if len(values) > 1 {
		for _, v := range values {
			req.Header.Add(key, strings.TrimSpace(v))
		}
	} else {
		req.Header.Add(key, val)
	}
	return hyper.IterContinue
}

func getBodyChunk(userdata c.Pointer, chunk *hyper.Buf) c.Int {
	fmt.Printf("getBodyChunk called\n")
	writer := (*io.PipeWriter)(userdata)
	buf := chunk.Bytes()
	len := chunk.Len()
	writer.Write(unsafe.Slice(buf, len))

	return hyper.IterContinue
}

func freeBodyWriter(userdata c.Pointer) {
	writer := (*io.PipeWriter)(userdata)
	writer.Close()
}
