package httpget

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
	fmt.Println("reading1...")
	resp := (*Response)(userdata)
	len := chunk.Len()
	buf := unsafe.Slice((*byte)(chunk.Bytes()), len)
	_, err := resp.respBodyWriter.Write(buf)
	resp.ContentLength += int64(len)
	if err != nil {
		fmt.Printf("Failed to write response body: %v\n", err)
		return hyper.IterBreak
	}
	fmt.Println("reading2...")
	return hyper.IterContinue
}

func (resp *Response) PrintBody() {
	var buffer = make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buffer)
		if err == io.EOF {
			fmt.Printf("\n")
			break
		}
		if err != nil {
			fmt.Println("Error reading from pipe:", err)
			break
		}
		fmt.Printf("%s", string(buffer[:n]))
	}
	buffer = nil
	//body, _ := io.ReadAll(resp.Content)
	//fmt.Println(string(body))
}
