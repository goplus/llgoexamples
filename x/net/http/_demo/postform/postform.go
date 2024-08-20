package main

import (
	"fmt"
	"io"
	"net/url"

	"github.com/goplus/llgoexamples/x/net/http"
)

func main() {
	formData := url.Values{
		"name":  {"John Doe"},
		"email": {"johndoe@example.com"},
	}

	resp, err := http.PostForm("http://httpbin.org/post", formData)
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
	fmt.Println(string(body))
	defer resp.Body.Close()
}
