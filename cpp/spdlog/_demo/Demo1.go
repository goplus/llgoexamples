package main

import (
	"github.com/goplus/llgo/c"
	"github.com/goplus/llgoexamples/cpp/spdlog"
)

func main() {
	spdlog.PrintInfo(c.Str("Hello World"))
	spdlog.PrintCritical(c.Str("This is a critical message"))
	spdlog.PrintError(c.Str("This is an error message"))
	//spdlog.Shutdown()
	spdlog.PrintWarn(c.Str("This is a warning message"))
	/*
	*SPDLOG_LEVEL_TRACE 0
	*SPDLOG_LEVEL_DEBUG 1
	*SPDLOG_LEVEL_INFO 2
	*SPDLOG_LEVEL_WARN 3
	*SPDLOG_LEVEL_ERROR 4
	*SPDLOG_LEVEL_CRITICAL 5
	*SPDLOG_LEVEL_OFF 6
	 */
	spdlog.PrintDebug(c.Str("This debug message should not be printed"))
	spdlog.SetLevel(1)
	spdlog.PrintDebug(c.Str("This debug messagen should be printed"))
	spdlog.PrintInfoWithInt(c.Str("Support for int :{}"), 100)
}
