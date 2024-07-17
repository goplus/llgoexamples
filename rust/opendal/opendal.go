package opendal

import (
	_ "unsafe"

	"github.com/goplus/llgo/c"
)

const (
	LLGoPackage = "link: $(pkg-config --libs opendal_c); -lopendal_c"
)

type Code c.Int

/**
 * \brief The error code for all opendal APIs in C binding.
 * \todo The error handling is not complete, the error with error message will be
 * added in the future.
 */
const (
	/*
	 * returning it back. For example, s3 returns an internal service error.
	 */
	Unexpected Code = iota
	/*
	 * Underlying service doesn't support this operation.
	 */
	Unsupported
	/*
	 * The config for backend is invalid.
	 */
	ConfigInvalid
	/*
	 * The given path is not found.
	 */
	NotFound
	/*
	 * The given path doesn't have enough permission for this operation
	 */
	PermissionDenied
	/*
	 * The given path is a directory.
	 */
	IsADirectory
	/*
	 * The given path is not a directory.
	 */
	NotADirectory
	/*
	 * The given path already exists thus we failed to the specified operation on it.
	 */
	AlreadyExists
	/*
	 * Requests that sent to this path is over the limit, please slow down.
	 */
	RateLimited
	/*
	 * The given file paths are same.
	 */
	IsSameFile
	/*
	 * The condition of this operation is not match.
	 */
	ConditionNotMatch
	/*
	 * The range of the content is not satisfied.
	 */
	RangeNotSatisfied
)

type ResultOperator struct {
	Op    *Operator
	Error *Error
}

type OperatorOptions struct {
	Inner *HashMapStringString
}

type Error struct {
	Code    Code
	Message Bytes
}

type Bytes struct {
	Data *uint8
	Len  c.Ulong
}

type ResultRead struct {
	Data  *Bytes
	Error *Error
}

type BlockingLister struct {
	Unused [0]byte
}

type BlockingOperator struct {
	Unused [0]byte
}

type Operator struct {
	Ptr *BlockingOperator
}

type InnerEntry struct {
	Unused [0]byte
}

type HashMapStringString struct {
	Unused [0]byte
}

type InnerMetadata struct {
	Unused [0]byte
}

type OperatorInfo struct {
	Unused [0]byte
}

type StdReader struct {
	Unused [0]byte
}

type Entry struct {
	Inner *InnerEntry
}

type ResultListerNext struct {
	Entry *Entry
	Error *Error
}

type Lister struct {
	Inner *BlockingLister
}

type MetaData struct {
	Inner *InnerMetadata
}

type Reader struct {
	Inner *StdReader
}

type ResultOperatorReader struct {
	Reader *Reader
	Error  *Error
}

type ResultIsExist struct {
	IsExist int8
	Error   *Error
}

type ResultStat struct {
	Meta  *MetaData
	Error *Error
}

// llgo:link NewResultOperator C.opendal_operator_new
func NewResultOperator(schema *c.Char, options *OperatorOptions) (rst ResultOperator) { return }

// llgo:link (*Operator).Write C.opendal_operator_write
func (op *Operator) Write(path *c.Char, bytes Bytes) *Error { return nil }

// llgo:link (*Operator).Read C.opendal_operator_read
func (op *Operator) Read(path *c.Char) (rst ResultRead) { return }

// llgo:link (*Bytes).Free C.opendal_bytes_free
func (bs *Bytes) Free() {}

// llgo:link (*Operator).Free C.opendal_operator_free
func (op *Operator) Free() {}

// llgo:link (*Error).Free C.opendal_error_free
func (err *Error) Free() {}
