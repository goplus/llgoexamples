package http

import (
	"fmt"
	"io"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgoexamples/rust/hyper"
)

type Response struct {
	Status          string
	StatusCode      int
	Header          Header
	Content         io.ReadCloser
	ContentLen      int64
	respBodyWriter  *io.PipeWriter
	ResponseBody    *uint8
	ResponseBodyLen uintptr
}

// AppendToResponseBody (BodyForEachCallback) appends the body to the response
//func AppendToResponseBody(userdata c.Pointer, chunk *hyper.Buf) c.Int {
//	resp := (*Response)(userdata)
//	len := chunk.Len()
//	buf := unsafe.Slice((*byte)(chunk.Bytes()), len)
//
//	if resp.Content == nil {
//		var reader *io.PipeReader
//		reader, resp.respBodyWriter = io.Pipe()
//		resp.Content = io.ReadCloser(reader)
//	}
//	resp.ContentLen += int64(len)
//	var err error
//	go func() {
//		_, err = resp.respBodyWriter.Write(buf)
//	}()
//	if err != nil {
//		fmt.Printf("Failed to write response body: %v\n", err)
//		return hyper.IterBreak
//	}
//	return hyper.IterContinue
//}

func (resp *Response) PrintBody1() {
	go func() {
		var reader *io.PipeReader
		reader, writer := io.Pipe()
		resp.Content = reader
		writer.Write((*[1 << 30]byte)(c.Pointer(resp.ResponseBody))[:resp.ResponseBodyLen:resp.ResponseBodyLen])
		defer writer.Close()
	}()
	for i := 0; i < 10; i++ {
		c.Usleep(1 * 1000 * 1000)
		fmt.Println("Sleeping...")
	}
	var buffer = make([]byte, 4096)
	for {
		n, err := resp.Content.Read(buffer)
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

// AppendToResponseBody (BodyForEachCallback) appends the body to the response
func AppendToResponseBody(userdata c.Pointer, chunk *hyper.Buf) c.Int {
	resp := (*Response)(userdata)
	buf := chunk.Bytes()
	len := chunk.Len()
	responseBody := (*uint8)(c.Malloc(resp.ResponseBodyLen + len))
	if responseBody == nil {
		c.Fprintf(c.Stderr, c.Str("Failed to allocate memory for response body\n"))
		return hyper.IterBreak
	}

	// Copy the existing response body to the new buffer
	if resp.ResponseBody != nil {
		c.Memcpy(c.Pointer(responseBody), c.Pointer(resp.ResponseBody), resp.ResponseBodyLen)
		c.Free(c.Pointer(resp.ResponseBody))
	}

	// Append the new data
	c.Memcpy(c.Pointer(uintptr(c.Pointer(responseBody))+resp.ResponseBodyLen), c.Pointer(buf), len)
	resp.ResponseBody = responseBody
	resp.ResponseBodyLen += len
	return hyper.IterContinue
}

func (resp *Response) PrintBody2() {
	//c.Printf(c.Str("%.*s\n"), c.Int(resp.ResponseBodyLen), resp.ResponseBody)
	fmt.Println(string((*[1 << 30]byte)(c.Pointer(resp.ResponseBody))[:resp.ResponseBodyLen:resp.ResponseBodyLen]))
}
