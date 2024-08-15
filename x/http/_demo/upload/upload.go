package main

import (
	"fmt"
	"io"

	"github.com/goplus/llgoexamples/x/http"
)

func main() {
	resp, err := http.Post("http://httpbin.org/post", "", nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(resp.Status)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(body))
	defer resp.Body.Close()
}
