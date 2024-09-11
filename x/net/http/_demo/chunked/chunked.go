package main

import (
	"fmt"
	"io"

	"github.com/goplus/llgoexamples/x/net/http"
)

func main() {
	resp, err := http.Get("http://localhost:8080/chunked")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()
	fmt.Println(resp.Status, "read bytes: ", resp.ContentLength)
	for key, values := range resp.Header {
		for _, value := range values {
			fmt.Printf("%s: %s\n", key, value)
		}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(body))
}
