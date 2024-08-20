package http

import (
	"fmt"
	"sync"
)

type ServeMux struct {
	mu sync.RWMutex
	m  map[string]muxEntry
}

type muxEntry struct {
	h       Handler
	pattern string
}

// DefaultServeMux is the default [ServeMux] used by [Serve].
var DefaultServeMux = &ServeMux{m: make(map[string]muxEntry)}

func (mux *ServeMux) ServeHTTP(w ResponseWriter, r *Request) {
	fmt.Printf("ServeHTTP called\n")
	h, _ := mux.Handler(r)
	h.ServeHTTP(w, r)
}

func (mux *ServeMux) Handler(r *Request) (h Handler, pattern string) {
	mux.mu.RLock()
	defer mux.mu.RUnlock()

	h, pattern = mux.m[r.URL.Path].h, r.URL.Path
	if h == nil {
		h, pattern = NotFoundHandler(), ""
	}
	return
}

func (mux *ServeMux) HandleFunc(pattern string, handler func(ResponseWriter, *Request)) {
	mux.Handle(pattern, HandlerFunc(handler))
}

func (mux *ServeMux) Handle(pattern string, handler Handler) {
	mux.mu.Lock()
	defer mux.mu.Unlock()

	if pattern == "" {
		panic("http: invalid pattern")
	}
	if handler == nil {
		panic("http: nil handler")
	}
	if _, exist := mux.m[pattern]; exist {
		panic("http: multiple registrations for " + pattern)
	}

	mux.m[pattern] = muxEntry{h: handler, pattern: pattern}
}