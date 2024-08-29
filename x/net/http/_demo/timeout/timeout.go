package main

import (
	"fmt"
	"io"
	"time"

	"github.com/goplus/llgoexamples/x/net/http"
)

func main() {
	client := &http.Client{
		//Timeout: time.Millisecond, // Set a small timeout to ensure it will time out
		Timeout: time.Second * 5,
	}
	req, err := http.NewRequest("GET", "https://www.baidu.com", nil)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	println(string(body))
	defer resp.Body.Close()
}
