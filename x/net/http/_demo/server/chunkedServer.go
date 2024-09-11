package main

import (
	"fmt"
	"net/http"
)

func chunkedHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("Content-Type", "text/plain")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	sentence := "This is a chunked encoded response. It will be sent in multiple parts. Note the delay between each section."

	words := []string{}
	start := 0
	for i, r := range sentence {
		if r == '。' || r == '，' || i == len(sentence)-1 {
			words = append(words, sentence[start:i+1])
			start = i + 1
		}
	}

	for _, word := range words {
		fmt.Fprintf(w, "%s", word)
		flusher.Flush()
	}
}

func main() {
	http.HandleFunc("/chunked", chunkedHandler)
	fmt.Println("Starting server on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Printf("Error starting server: %s\n", err)
	}
}