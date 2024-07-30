package main

import (
	"fmt"

	"github.com/goplus/llgoexamples/x/http"
)

func main() {
	// 使用 http.Get 发送 GET 请求
	resp, err := http.Get("https://www.baidu.com/")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(resp.Status)
	fmt.Println(resp.StatusCode)
	resp.PrintHeaders()
	fmt.Println()
	resp.PrintBody2()

	resp.PrintBody1()
	defer resp.Content.Close()
}
