package main

import (
	"fmt"
	"io"

	"github.com/goplus/llgoexamples/x/net/http"
)

func main() {
	resp, err := http.Get("https://www.baidu.com")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()
	fmt.Println(resp.Status, "read bytes: ", resp.ContentLength)
	resp.PrintHeaders()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(body))
}
