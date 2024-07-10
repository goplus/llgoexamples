package opendal

import (
	_ "unsafe"

	"github.com/goplus/llgo/c"
)

const (
	LLGoPackage = "link: $(pkg-config --libs opendal_c); -lopendal_c"
)

type ResultOperator struct {
	Op    *Operator
	Error *Error
}

type OperatorOptions struct {
	Unused [8]byte
}

type Error struct {
	Unused [8]byte
}

type Operator struct {
	Unused [8]byte
}

type Bytes struct {
	Data *uint8
	Len  c.Ulong
}

type ResultRead struct {
	Data  *Bytes
	Error *Error
}

// llgo:link NewResultOperator C.opendal_operator_new
func NewResultOperator(schema *c.Char, options *OperatorOptions) (rst ResultOperator) { return }

// llgo:link (*Operator).OperatorWrite C.opendal_operator_write
func (op *Operator) OperatorWrite(path *c.Char, bytes Bytes) *Error { return nil }

// llgo:link (*Operator).OperatorRead C.opendal_operator_read
func (op *Operator) OperatorRead(path *c.Char) (rst ResultRead) { return }

// llgo:link (*Bytes).FreeBytes C.opendal_bytes_free
func (bs *Bytes) FreeBytes() {}

// llgo:link (*Operator).FreeOperator C.opendal_operator_free
func (op *Operator) FreeOperator() {}
