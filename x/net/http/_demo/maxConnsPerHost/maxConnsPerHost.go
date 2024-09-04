package main

import (
	"fmt"
	"io"

	"github.com/goplus/llgoexamples/x/net/http"
)

func main() {
	client := &http.Client{
		Transport: &http.Transport{
			MaxConnsPerHost: 2,
		},
	}
	req, err := http.NewRequest("GET", "https://www.baidu.com", nil)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()
	fmt.Println(resp.Status, "read bytes: ", resp.ContentLength)
	fmt.Println(resp.Proto)
	resp.PrintHeaders()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(body))
}
