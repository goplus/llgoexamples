package main

import (
	"fmt"
	"io"

	"github.com/goplus/llgoexamples/x/net/http"
)

func main() {
	resp, err := http.Get("http://localhost:8080") // Start "../server/redirectServer.go" before running
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(resp.Status, "read bytes: ", resp.ContentLength)
	fmt.Println(resp.Proto)
	resp.PrintHeaders()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(body))
	defer resp.Body.Close()
}
