package main

import (
	"fmt"
	//"io"

	"github.com/goplus/llgo/x/net/http"
)

func echoHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("echoHandler called\n")
	//TODO(hackerchai): read body and echo
	// fmt.Printf("> %s %s HTTP/%d.%d\n", r.Method, r.RequestURI, r.ProtoMajor, r.ProtoMinor)
	// for key, values := range r.Header {
	// 	for _, value := range values {
	// 		fmt.Printf("> %s: %s\n", key, value)
	// 	}
	// }
	// fmt.Printf("URL: %s\n", r.URL.String())
	// // fmt.Println("ContentLength: %d", r.ContentLength)
	// // fmt.Println("TransferEncoding: %s", r.TransferEncoding)
	// //TODO: read body and echo
	// body, err := io.ReadAll(r.Body)
	// println("body read")

	// if err != nil {
	// 	http.Error(w, "Error reading request body", http.StatusInternalServerError)
	// 	return
	// }
	// defer r.Body.Close()
	// fmt.Printf("body read")
	// w.Header().Set("Content-Type", "text/plain")
	// w.Write(body)

	fmt.Printf("> %s %s HTTP/%d.%d\n", r.Method, r.RequestURI, r.ProtoMajor, r.ProtoMinor)
	for key, values := range r.Header {
		for _, value := range values {
			fmt.Printf("> %s: %s\n", key, value)
		}
	}
	fmt.Printf("URL: %s\n", r.URL.String())
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("hello world\n"))
}

func main() {
	http.HandleFunc("/echo", echoHandler)

	fmt.Println("Starting server on :1234")
	server := http.NewServer("127.0.0.1:1234")
	if err := server.ListenAndServe(); err != nil {
		panic(err)
	}
}
