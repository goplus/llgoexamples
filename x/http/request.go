package http

import (
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/os"
	"github.com/goplus/llgoexamples/rust/hyper"
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

type postBody struct {
	data    []byte
	len     uintptr
	readLen uintptr
}

type uploadBody struct {
	fd  c.Int
	buf []byte
	len uintptr
}

var DefaultChunkSize uintptr = 8192

func NewRequest(method, urlStr string, body io.Reader) (*Request, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	//rc, ok := body.(io.ReadCloser)
	//if !ok && body != nil {
	//	rc = io.NopCloser(body)
	//}
	request := &Request{
		Method:     method,
		URL:        u,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(Header),
		Host:       u.Host,
		//Body:       rc,
		timeout: 0,
	}
	request.Header.Set("Host", request.Host)

	return request, nil
}

func PrintInformational(userdata c.Pointer, resp *hyper.Response) {
	status := resp.Status()
	fmt.Println("Informational (1xx): ", status)
}

func SetPostData(userdata c.Pointer, ctx *hyper.Context, chunk **hyper.Buf) c.Int {
	//upload := (*uploadBody)(userdata)
	//res := os.Read(upload.fd, c.Pointer(&upload.buf[0]), upload.len)
	//if res > 0 {
	//	*chunk = hyper.CopyBuf(&upload.buf[0], uintptr(res))
	//	return hyper.PollReady
	//}
	//if res == 0 {
	//	*chunk = nil
	//	os.Close(upload.fd)
	//	return hyper.PollReady
	//}
	body := (*postBody)(userdata)
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

	fmt.Printf("error reading upload file: %s\n", c.GoString(c.Strerror(os.Errno)))
	return hyper.PollError
}

func newHyperRequest(req *Request) (*hyper.Request, error) {
	host := req.Host
	uri := req.URL.Path
	method := req.Method
	// Prepare the request
	hyperReq := hyper.NewRequest()
	// Set the request method and uri
	if hyperReq.SetMethod(&[]byte(method)[0], c.Strlen(c.AllocaCStr(method))) != hyper.OK {
		return nil, fmt.Errorf("error setting method %s\n", method)
	}
	if hyperReq.SetURI(&[]byte(uri)[0], c.Strlen(c.AllocaCStr(uri))) != hyper.OK {
		return nil, fmt.Errorf("error setting uri %s\n", uri)
	}
	// Set the request headers
	reqHeaders := hyperReq.Headers()
	if reqHeaders.Set(&[]byte("Host")[0], c.Strlen(c.Str("Host")), &[]byte(host)[0], c.Strlen(c.AllocaCStr(host))) != hyper.OK {
		return nil, fmt.Errorf("error setting header: Host: %s\n", host)
	}

	if method == "POST" {
		//var upload uploadBody
		//upload.fd = os.Open(c.Str("/Users/spongehah/go/src/llgo/x/http/_demo/post/example.txt"), os.O_RDONLY)
		//if upload.fd < 0 {
		//	return nil, fmt.Errorf("error opening file to upload: %s\n", c.GoString(c.Strerror(os.Errno)))
		//}
		//upload.len = 8192
		//upload.buf = make([]byte, upload.len)
		req.Header.Set("expect", "100-continue")
		hyperReq.OnInformational(PrintInformational, nil)
		postData := []byte(`{"id":1,"title":"foo","body":"bar","userId":"1"}`)

		reqBody := &postBody{
			data: postData,
			len:  uintptr(len(postData)),
		}

		hyperReqBody := hyper.NewBody()
		hyperReqBody.SetUserdata(c.Pointer(reqBody))
		//hyperReqBody.SetUserdata(c.Pointer(&upload))
		hyperReqBody.SetDataFunc(SetPostData)
		hyperReq.SetBody(hyperReqBody)
	}

	// Add user-defined request headers to hyper.Request
	err := req.setHeaders(hyperReq)
	if err != nil {
		return nil, err
	}

	return hyperReq, nil
}

// setHeaders sets the headers of the request
func (req *Request) setHeaders(hyperReq *hyper.Request) error {
	headers := hyperReq.Headers()
	for key, values := range req.Header {
		valueLen := len(values)
		if valueLen > 1 {
			for _, value := range values {
				if headers.Add(&[]byte(key)[0], c.Strlen(c.AllocaCStr(key)), &[]byte(value)[0], c.Strlen(c.AllocaCStr(value))) != hyper.OK {
					return fmt.Errorf("error adding header %s: %s\n", key, value)
				}
			}
		} else if valueLen == 1 {
			if headers.Set(&[]byte(key)[0], c.Strlen(c.AllocaCStr(key)), &[]byte(values[0])[0], c.Strlen(c.AllocaCStr(values[0]))) != hyper.OK {
				return fmt.Errorf("error setting header %s: %s\n", key, values[0])
			}
		} else {
			return fmt.Errorf("error setting header %s: empty value\n", key)
		}
	}
	return nil
}

func (r *Request) closeBody() error {
	if r.Body == nil {
		return nil
	}
	return r.Body.Close()
}
