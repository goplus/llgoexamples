package main

import (
	"github.com/goplus/llgo/c"
	"github.com/goplus/llgoexamples/rust/opendal"
)

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

	/* Prepare some data to be written */
	data := opendal.Bytes{
		Data: (*uint8)(c.Pointer(c.Str("this_string_length_is_24"))),
		Len:  c.Ulong(24),
	}

	/* Write this into path "/testpath" */
	error := op.OperatorWrite(c.Str("/testpath"), data)
	if error != nil {
		return
	}

	/* We can read it out, make sure the data is the same */
	r := op.OperatorRead(c.Str("/testpath"))
	readBytes := r.Data
	if r.Error != nil {
		return
	}
	if readBytes.Len != 24 {
		return
	}

	/* Lets print it out */
	dataSlice := (*[1 << 6]uint8)(c.Pointer(readBytes.Data))[:readBytes.Len:readBytes.Len]

	for i := 0; i < 24; i++ {
		c.Printf(c.Str("%c"), dataSlice[i])
	}

	c.Printf(c.Str("\n"))

	/* the opendal_bytes read is heap allocated, please free it */
	readBytes.FreeBytes()
	/* the operator_ptr is also heap allocated */
	op.FreeOperator()
}
