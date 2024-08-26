package main

import (
	"fmt"
	"io"
	"os"

	"github.com/goplus/llgoexamples/x/net/http"
)

func main() {
	url := "http://httpbin.org/post"
	//url := "http://localhost:8080"
	filePath := "/Users/spongehah/go/src/llgo/x/net/http/_demo/upload/example.txt" // Replace with your file path
	//filePath := "/Users/spongehah/Downloads/xiaoshuo.txt" // Replace with your file path

	file, err := os.Open(filePath)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	client := &http.Client{}
	req, err := http.NewRequest("POST", url, file)
	if err != nil {
		fmt.Println(err)
		return
	}
	req.Header.Set("expect", "100-continue")
	resp, err := client.Do(req)

	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()
	fmt.Println("Status:", resp.Status)
	resp.PrintHeaders()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(respBody))
}
