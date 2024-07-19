package main

import (
	"github.com/goplus/llgo/rust/hyper"
)

func main() {
	config := &hyper.RequestConfig{
		ReqHost: "httpbin.org",
		ReqPort: "80",
		ReqUri:  "/",
	}

	response := hyper.SendRequest(config)
	println()
	println(response.Status, response.Message)
}
