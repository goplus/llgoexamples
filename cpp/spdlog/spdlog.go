package spdlog

import (
	_ "unsafe"

	"github.com/goplus/llgo/c"
)

const (
	LLGoFiles   = "$(pkg-config --cflags spdlog): _wrap/spdlog.cpp"
	LLGoPackage = "link: $(pkg-config --libs spdlog); -lspdlog -pthread -lfmt -lc++"
)

//go:linkname PrintInfo C.PrintInfo
func PrintInfo(msg *c.Char)

//go:linkname PrintCritical C.PrintCritical
func PrintCritical(msg *c.Char)

//go:linkname PrintError C.PrintError
func PrintError(msg *c.Char)

//go:linkname PrintWarn C.PrintWarn
func PrintWarn(msg *c.Char)

//go:linkname PrintDebug C.PrintDebug
func PrintDebug(msg *c.Char)

//go:linkname Shutdown C._ZN6spdlog8shutdownEv
func Shutdown()

//go:linkname SetLevel C._ZN6spdlog9set_levelENS_5level10level_enumE
func SetLevel(level c.Int)

//go:linkname PrintInfoWithInt C.PrintInfoWithInt
func PrintInfoWithInt(msg *c.Char, i c.Int)
