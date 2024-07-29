package http

import (
	"fmt"
	"io"
	"unsafe"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgoexamples/rust/hyper"
)

type Response struct {
	Status          string
	StatusCode      int
	Header          Header
	ResponseBody    io.ReadCloser
	respBodyWriter  *io.PipeWriter
	ResponseBodyLen int64
}

// AppendToResponseBody (BodyForEachCallback) appends the body to the response
func AppendToResponseBody(userdata c.Pointer, chunk *hyper.Buf) c.Int {
	resp := (*Response)(userdata)
	len := chunk.Len()
	buf := unsafe.Slice((*byte)(chunk.Bytes()), len)

	if resp.ResponseBody == nil {
		var reader *io.PipeReader
		reader, resp.respBodyWriter = io.Pipe()
		resp.ResponseBody = io.ReadCloser(reader)
	}
	resp.ResponseBodyLen += int64(len)
	var err error
	go func() {
		_, err = resp.respBodyWriter.Write(buf)
	}()
	if err != nil {
		fmt.Printf("Failed to write response body: %v\n", err)
		return hyper.IterBreak
	}
	return hyper.IterContinue
}

func (resp *Response) PrintBody() {
	var buffer = make([]byte, resp.ResponseBodyLen)
	for {
		n, err := resp.ResponseBody.Read(buffer)
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
}

//// AppendToResponseBody (BodyForEachCallback) appends the body to the response
//func AppendToResponseBody(userdata c.Pointer, chunk *hyper.Buf) c.Int {
//	resp := (*Response)(userdata)
//	buf := chunk.Bytes()
//	len := chunk.Len()
//	responseBody := (*uint8)(c.Malloc(resp.ResponseBodyLen + len))
//	if responseBody == nil {
//		c.Fprintf(c.Stderr, c.Str("Failed to allocate memory for response body\n"))
//		return hyper.IterBreak
//	}
//
//	// Copy the existing response body to the new buffer
//	if resp.ResponseBody != nil {
//		c.Memcpy(c.Pointer(responseBody), c.Pointer(resp.ResponseBody), resp.ResponseBodyLen)
//		c.Free(c.Pointer(resp.ResponseBody))
//	}
//
//	// Append the new data
//	c.Memcpy(c.Pointer(uintptr(c.Pointer(responseBody))+resp.ResponseBodyLen), c.Pointer(buf), len)
//	resp.ResponseBody = responseBody
//	resp.ResponseBodyLen += len
//	return hyper.IterContinue
//}

//func (resp *Response) PrintBody() {
//	//c.Printf(c.Str("%.*s\n"), c.Int(resp.ResponseBodyLen), resp.ResponseBody)
//	fmt.Println(string((*[1 << 30]byte)(c.Pointer(resp.ResponseBody))[:resp.ResponseBodyLen:resp.ResponseBodyLen]))
//}
