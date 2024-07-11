package main

import (
	"github.com/goplus/llgo/c"
	"github.com/goplus/llgoexamples/rust/opendal"
)

// this example shows how to get error message from opendal_error
func main() {
	/* Initialize a operator for "memory" backend, with no options */
	result := opendal.NewResultOperator(c.Str("memory"), nil)
	if result.Op == nil {
		return
	}
	if result.Error != nil {
		return
	}

	op := result.Op

	/* The read is supposed to fail */
	r := op.Read(c.Str("testpath"))
	if r.Error == nil {
		return
	}
	if r.Error.Code != opendal.NotFound {
		return
	}

	/* Lets print the error message out */
	errorMsg := &r.Error.Message
	dataSlice := (*[1 << 30]uint8)(c.Pointer(errorMsg.Data))[:errorMsg.Len:errorMsg.Len]
	for i := 0; i < int(errorMsg.Len); i++ {
		c.Printf(c.Str("%c"), dataSlice[i])
	}

	/* free the error since the error is not NULL */
	r.Error.Free()

	/* the operator_ptr is also heap allocated */
	op.Free()
}
