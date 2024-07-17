package spdlog

import (
	_ "unsafe"

	"github.com/goplus/llgo/c"
)

const (
	LLGoFiles   = "$(pkg-config --cflags spdlog): _wrap/spdlog.cpp"
	LLGoPackage = "link: $(pkg-config --libs spdlog); -lspdlog -pthread -lfmt -lc++"
)

//go:linkname SpdlogPrintInfo C.PrintInfo
func SpdlogPrintInfo(msg *c.Char)

//go:linkname SpdlogPrintCritical C.PrintCritical
func SpdlogPrintCritical(msg *c.Char)

//go:linkname SpdlogPrintError C.PrintError
func SpdlogPrintError(msg *c.Char)

//go:linkname SpdlogPrintWarn C.PrintWarn
func SpdlogPrintWarn(msg *c.Char)

//go:linkname SpdlogPrintDebug C.PrintDebug
func SpdlogPrintDebug(msg *c.Char)

//go:linkname SpdlogShutdown C._ZN6spdlog8shutdownEv
func SpdlogShutdown()

//go:linkname SpdlogSetLevel C._ZN6spdlog9set_levelENS_5level10level_enumE
func SpdlogSetLevel(level c.Int)

//go:linkname SpdlogPrintInfoWithInt C.PrintInfoWithInt
func SpdlogPrintInfoWithInt(msg *c.Char, i c.Int)
