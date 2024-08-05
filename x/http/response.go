package http

import (
	"fmt"
	"io"
	"unsafe"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgoexamples/rust/hyper"
)

type Response struct {
	Status         string
	StatusCode     int
	Header         Header
	Body           io.ReadCloser
	ContentLength  int64
	respBodyWriter *io.PipeWriter
}

// AppendToResponseBody (BodyForEachCallback) appends the body to the response
func AppendToResponseBody(userdata c.Pointer, chunk *hyper.Buf) c.Int {
	resp := (*Response)(userdata)
	len := chunk.Len()
	buf := unsafe.Slice((*byte)(chunk.Bytes()), len)
	_, err := resp.respBodyWriter.Write(buf)
	resp.ContentLength += int64(len)
	if err != nil {
		fmt.Printf("Failed to write response body: %v\n", err)
		return hyper.IterBreak
	}
	return hyper.IterContinue
}
