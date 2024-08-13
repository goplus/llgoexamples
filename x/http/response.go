package http

import (
	"fmt"
	"io"
	"strconv"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgoexamples/rust/hyper"
)

type Response struct {
	Status        string // e.g. "200 OK"
	StatusCode    int    // e.g. 200
	Proto         string // e.g. "HTTP/1.0"
	ProtoMajor    int    // e.g. 1
	ProtoMinor    int    // e.g. 0
	Header        Header
	Body          io.ReadCloser
	ContentLength int64
	Trailer       Header
	Chunked       bool
	Request       *Request
}

func readResponseLineAndHeader(resp *Response, hyperResp *hyper.Response) {
	rp := hyperResp.ReasonPhrase()
	rpLen := hyperResp.ReasonPhraseLen()

	resp.Status = strconv.Itoa(int(hyperResp.Status())) + " " + string((*[1 << 30]byte)(c.Pointer(rp))[:rpLen:rpLen])
	resp.StatusCode = int(hyperResp.Status())

	version := int(hyperResp.Version())
	resp.ProtoMajor, resp.ProtoMinor = splitTwoDigitNumber(version)
	resp.Proto = fmt.Sprintf("HTTP/%d.%d", resp.ProtoMajor, resp.ProtoMinor)

	headers := hyperResp.Headers()
	headers.Foreach(AppendToResponseHeader, c.Pointer(resp))
}
