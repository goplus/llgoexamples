package main

import (
	"fmt"
	"io"

	"github.com/goplus/llgoexamples/x/net/http"
)

func main() {
	client := &http.Client{}
	req, err := http.NewRequest("GET", "https://www.baidu.com", nil)
	if err != nil {
		println(err.Error())
		return
	}

	//req.Header.Set("accept", "*/*")
	req.Header.Set("accept-encoding", "gzip")
	//req.Header.Set("cache-control", "no-cache")
	//req.Header.Set("pragma", "no-cache")
	//req.Header.Set("priority", "u=0, i")
	//req.Header.Set("referer", "https://jsonplaceholder.typicode.com/")
	//req.Header.Set("sec-ch-ua", "\"Not)A;Brand\";v=\"99\", \"Google Chrome\";v=\"127\", \"Chromium\";v=\"127\"")
	//req.Header.Set("sec-ch-ua-mobile", "?0")
	//req.Header.Set("sec-ch-ua-platform", "\"macOS\"")
	//req.Header.Set("sec-fetch-dest", "document")
	//req.Header.Set("sec-fetch-mode", "navigate")
	//req.Header.Set("sec-fetch-site", "same-origin")
	//req.Header.Set("sec-fetch-user", "?1")
	////req.Header.Set("upgrade-insecure-requests", "1")
	//req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/127.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		println(err.Error())
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
		println(err.Error())
		return
	}
	fmt.Println(string(body))
}
