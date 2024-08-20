package main

import (
	"fmt"
	"io"
	"os"

	"github.com/goplus/llgoexamples/x/net/http"
)

func main() {
	url := "http://httpbin.org/post"
	filePath := "/Users/spongehah/go/src/llgo/x/http/_demo/upload/example.txt" // Replace with your file path

	file, err := os.Open(filePath)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	resp, err := http.Post(url, "application/octet-stream", file)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(respBody))
}
