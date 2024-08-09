package main

import (
	"fmt"
	"io"
	"time"

	"github.com/goplus/llgo/x/http"
)

func main() {
	client := &http.Client{
		Timeout: time.Microsecond,
	}
	req, _ := http.NewRequest("GET", "https://www.baidu.com", nil)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	body, _ := io.ReadAll(resp.Body)
	println(string(body))
}
