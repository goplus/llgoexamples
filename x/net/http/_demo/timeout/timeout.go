package main

import (
	"fmt"
	"io"
	"time"

	"github.com/goplus/llgoexamples/x/net/http"
)

func main() {
	client := &http.Client{
		Timeout: time.Millisecond, // Set a small timeout to ensure it will time out
		//Timeout: time.Second,
	}
	req, err := http.NewRequest("GET", "https://www.baidu.com", nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return
	}
	println(string(body))
}
