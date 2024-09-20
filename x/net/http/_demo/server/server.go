package main

import (
	"fmt"

	"github.com/goplus/llgo/x/net/http"
)

func echoHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("[debug] echoHandler called\n")
	fmt.Printf(">> %s %s HTTP/%d.%d\n", r.Method, r.RequestURI, r.ProtoMajor, r.ProtoMinor)
	for key, values := range r.Header {
		for _, value := range values {
			fmt.Printf(">> %s: %s\n", key, value)
		}
	}
	fmt.Printf(">> URL: %s\n", r.URL.String())
	fmt.Printf(">> RemoteAddr: %s\n", r.RemoteAddr)

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("Hello, World!"))

	//TODO(hackerchai): temporarily unable to do blocking operation in handler due to imperfections of goroutine
	// body, err := io.ReadAll(r.Body)
	// if err != nil {
	// 	http.Error(w, "Error reading request body", http.StatusInternalServerError)
	// 	return
	// }
	// defer r.Body.Close()
	// fmt.Printf(">> Body: %s\n", string(body))

	// w.Header().Set("Content-Type", "text/plain")
	// w.Write(body)
}

func main() {
	http.HandleFunc("/echo", echoHandler)

	fmt.Println("Starting server on :1234")
	server := http.NewServer("127.0.0.1:1234")
	if err := server.ListenAndServe(); err != nil {
		panic(err)
	}
}
