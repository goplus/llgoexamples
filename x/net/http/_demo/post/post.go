package main

import (
	"bytes"
	"fmt"
	"io"

	"github.com/goplus/llgoexamples/x/net/http"
)

func main() {
	data := []byte(`{"id":1,"title":"foo","body":"bar","userId":"1"}`)
	resp, err := http.Post("https://jsonplaceholder.typicode.com/posts", "application/json; charset=UTF-8", bytes.NewBuffer(data))
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()
	fmt.Println(resp.Status)
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
