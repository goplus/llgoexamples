package http

import (
	"fmt"
	"io"
	"strconv"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgoexamples/rust/hyper"
)

type Response struct {
	Status           string // e.g. "200 OK"
	StatusCode       int    // e.g. 200
	Proto            string // e.g. "HTTP/1.0"
	ProtoMajor       int    // e.g. 1
	ProtoMinor       int    // e.g. 0
	Header           Header
	Body             io.ReadCloser
	ContentLength    int64
	TransferEncoding []string
	Close            bool
	//Trailer          Header
	Request *Request
}

func (r *Response) closeBody() {
	if r.Body != nil {
		r.Body.Close()
	}
}

func ReadResponse(hyperResp *hyper.Response, req *Request) (*Response, error) {
	resp := &Response{
		Request: req,
		Header:  make(Header),
		//Trailer: make(Header),
	}
	readResponseLineAndHeader(resp, hyperResp)

	fixPragmaCacheControl(req.Header)

	err := readTransfer(resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// readResponseLineAndHeader reads the response line and header from hyper response.
func readResponseLineAndHeader(resp *Response, hyperResp *hyper.Response) {
	rp := hyperResp.ReasonPhrase()
	rpLen := hyperResp.ReasonPhraseLen()

	resp.Status = strconv.Itoa(int(hyperResp.Status())) + " " + c.GoString((*int8)(c.Pointer(rp)), rpLen)
	resp.StatusCode = int(hyperResp.Status())

	version := int(hyperResp.Version())
	resp.ProtoMajor, resp.ProtoMinor = splitTwoDigitNumber(version)
	resp.Proto = fmt.Sprintf("HTTP/%d.%d", resp.ProtoMajor, resp.ProtoMinor)

	headers := hyperResp.Headers()
	headers.Foreach(appendToResponseHeader, c.Pointer(resp))
}

// RFC 7234, section 5.4: Should treat
//
//	Pragma: no-cache
//
// like
//
//	Cache-Control: no-cache
func fixPragmaCacheControl(header Header) {
	if hp, ok := header["Pragma"]; ok && len(hp) > 0 && hp[0] == "no-cache" {
		if _, presentcc := header["Cache-Control"]; !presentcc {
			header["Cache-Control"] = []string{"no-cache"}
		}
	}
}

// Cookies parses and returns the cookies set in the Set-Cookie headers.
func (r *Response) Cookies() []*Cookie {
	return readSetCookies(r.Header)
}