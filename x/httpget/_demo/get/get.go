package main

import (
	"fmt"
	"io"

	"github.com/goplus/llgo/x/httpget"
)

func main() {
	resp, err := httpget.Get("www.baidu.com")
	//req, _ := httpget.NewRequest("GET", "http://www.baidu.com", nil)
	//resp, err := httpget.DefaultClient.Send(req, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(body))
}
