package main

import (
	"fmt"
	"io"

	"github.com/goplus/llgo/x/net/http"
)

func echoHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusInternalServerError)
		return
	}
	//defer r.Body.Close()

	w.Header().Set("Content-Type", "text/plain")

	w.Write(body)
}

func main() {
	http.HandleFunc("/echo", echoHandler)

	fmt.Println("Starting server on :1234")
	server := http.NewServer("127.0.0.1:1234")
	if err := server.ListenAndServe(); err != nil {
		panic(err)
	}
}
