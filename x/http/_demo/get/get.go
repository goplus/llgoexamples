package main

import (
	"fmt"

	"github.com/goplus/llgoexamples/x/http"
)

func main() {
	// 使用 http.Get 发送 GET 请求
	resp := http.Get("https://www.baidu.com/")
	fmt.Println(resp.Status)
	fmt.Println(resp.StatusCode)
	resp.PrintHeaders()
	fmt.Println()
	resp.PrintBody()
}
