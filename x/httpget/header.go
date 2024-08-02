package httpget

import (
	"fmt"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgoexamples/rust/hyper"
)

type Header map[string][]string

// AppendToResponseHeader (HeadersForEachCallback) prints each header to the console
func AppendToResponseHeader(userdata c.Pointer, name *uint8, nameLen uintptr, value *uint8, valueLen uintptr) c.Int {
	resp := (*Response)(userdata)
	nameStr := string((*[1 << 30]byte)(c.Pointer(name))[:nameLen:nameLen])
	valueStr := string((*[1 << 30]byte)(c.Pointer(value))[:valueLen:valueLen])

	if resp.Header == nil {
		resp.Header = make(map[string][]string)
	}
	resp.Header[nameStr] = append(resp.Header[nameStr], valueStr)
	//c.Printf(c.Str("%.*s: %.*s\n"), int(nameLen), name, int(valueLen), value)
	return hyper.IterContinue
}

func (resp *Response) PrintHeaders() {
	for key, values := range resp.Header {
		for _, value := range values {
			fmt.Printf("%s: %s\n", key, value)
		}
	}
}
