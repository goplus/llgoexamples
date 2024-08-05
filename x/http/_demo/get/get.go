package main

import (
	"fmt"
	"io"

	"github.com/goplus/llgoexamples/x/http"
)

func main() {
	resp, err := http.Get("https://www.baidu.com")
	if err != nil {
		fmt.Println(err)
		return
	}
	println(resp.Status)
	resp.PrintHeaders()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(body))
	defer resp.Body.Close()
}
