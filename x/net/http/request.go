package http

import (
	"fmt"
	"io"

	//"mime/multipart"
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
	// Form             url.Values
	// PostForm         url.Values
	// MultipartForm    *multipart.Form
	RemoteAddr string
	RequestURI string
	timeout    time.Duration
}

func (conn *conn) readRequest(hyperReq *hyper.Request) (*Request, error) {
	println("[debug] readRequest called")
	req := Request{
		Header:  make(Header),
		timeout: 0,
		Body:    nil,
	}
	req.RemoteAddr = conn.remoteAddr

	headers := hyperReq.Headers()
	if headers != nil {
		headers.Foreach(addHeader, unsafe.Pointer(&req))
	} else {
		return nil, fmt.Errorf("failed to get request headers")
	}

	var host string
	for key, values := range req.Header {
		if strings.EqualFold(key, "Host") {
			if len(values) > 0 {
				host = values[0]
				break
			}
		}

	}

	method := make([]byte, 32)
	methodLen := unsafe.Sizeof(method)
	if err := hyperReq.Method(&method[0], &methodLen); err != hyper.OK {
		return nil, fmt.Errorf("failed to get method: %v", err)
	}

	methodStr := string(method[:methodLen])

	var scheme, authority, pathAndQuery [1024]byte
	schemeLen, authorityLen, pathAndQueryLen := unsafe.Sizeof(scheme), unsafe.Sizeof(authority), unsafe.Sizeof(pathAndQuery)
	uriResult := hyperReq.URIParts(&scheme[0], &schemeLen, &authority[0], &authorityLen, &pathAndQuery[0], &pathAndQueryLen)
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
		authorityStr = host
	} else {
		authorityStr = string(authority[:authorityLen])
	}

	if pathAndQueryLen == 0 {
		return nil, fmt.Errorf("failed to get URI path and query: %v", uriResult)
	} else {
		pathAndQueryStr = string(pathAndQuery[:pathAndQueryLen])
	}
	req.Host = authorityStr
	req.Method = methodStr
	req.RequestURI = pathAndQueryStr

	var proto string
	var protoMajor, protoMinor int
	version := hyperReq.Version()
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
	req.Proto = proto
	req.ProtoMajor = protoMajor
	req.ProtoMinor = protoMinor

	urlStr := fmt.Sprintf("%s://%s%s", schemeStr, authorityStr, pathAndQueryStr)
	url, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	req.URL = url

	body := hyperReq.Body()
	if body != nil {
		task := body.Data()
		taskFlag := getBodyTask
		taskData := taskData{
			hyperBody:    body,
			responseBody: nil,
			conn:         conn,
			taskFlag:     taskFlag,
		}
		task.SetUserdata(c.Pointer(&taskData), nil)
		requestBody := newRequestBody(conn.asyncHandle)
		conn.requestBody = requestBody
		req.Body = requestBody

		conn.asyncHandle.SetData(c.Pointer(&taskData))
		fmt.Println("[debug] async task set")
		if task != nil {
			r := conn.executor.Push(task)
			if r != hyper.OK {
				fmt.Printf("failed to push body foreach task: %d\n", r)
				task.Free()
				return nil, fmt.Errorf("failed to push body foreach task: %v", r)
			}
		} else {
			return nil, fmt.Errorf("failed to create body foreach task")
		}

	} else {
		return nil, fmt.Errorf("failed to get request body")
	}

	hyperReq.Free()

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
