package inih

import (
	_ "unsafe"

	"github.com/goplus/llgo/c"
)

// llgo:type C
type IniReader struct {
	Unused [24]byte
}

func Str(s string) *stdstring {
	var r stdstring
	r.init(c.GoStringData(s), c.Int(len(s)))
	return &r
}

type stdstring struct {
	buf [24]byte
}

// llgo:link (*stdstring).init C._ZNSt3__112basic_stringIcNS_11char_traitsIcEENS_9allocatorIcEEE6__initEPKcm
func (*stdstring) init(s *c.Char, size c.Int) {}

//go:linkname NewIniReader C._ZN9INIReaderC1EPKcm
func NewIniReader(fileName *c.Char, size c.Ulong) IniReader

//go:linkname NewIniReaderFile C._ZN9INIReaderC1ERKNSt3__112basic_stringIcNS0_11char_traitsIcEENS0_9allocatorIcEEEE
func NewIniReaderFile(fileName *stdstring) IniReader

// llgo:link (*IniReader).ParseError C._ZNK9INIReader10ParseErrorEv
func (*IniReader) ParseError() c.Int { return 0 }

// llgo:link (*IniReader).GetInteger C._ZNK9INIReader10GetIntegerERKNSt3__112basic_stringIcNS0_11char_traitsIcEENS0_9allocatorIcEEEES8_l
func (*IniReader) GetInteger(section *stdstring, name *stdstring, defaultValue c.Long) c.Long {
	return 0
}

// llgo:link (*IniReader).GetBoolean C._ZNK9INIReader10GetBooleanERKNSt3__112basic_stringIcNS0_11char_traitsIcEENS0_9allocatorIcEEEES8_b
func (*IniReader) GetBoolean(section *stdstring, name *stdstring, defaultValue bool) bool {
	return false
}
