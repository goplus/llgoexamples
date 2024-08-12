package main

import (
	"fmt"

	"github.com/goplus/llgo/x/textproto"
)

func main() {
	m := make(map[string][]string)
	textproto.MIMEHeader(m).Add("Host", "www.example.com")
	fmt.Println(textproto.MIMEHeader(m).Get("host"))
}

/*
Expected Output:
www.example.com
*/
