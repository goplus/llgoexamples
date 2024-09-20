package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/", handleInitialRequest)
	http.HandleFunc("/redirect", handleRedirectRequest)

	fmt.Println("Server is running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleInitialRequest(w http.ResponseWriter, r *http.Request) {
	log.Println("Received initial request, redirecting...")
	http.Redirect(w, r, "/redirect", http.StatusSeeOther)
}

func handleRedirectRequest(w http.ResponseWriter, r *http.Request) {
	log.Println("Received redirect request, sending response...")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "Hello redirect")
}
