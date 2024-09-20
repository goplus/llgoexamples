package http

import (
	"sync"
)

// ServeMux is a HTTP request multiplexer
type ServeMux struct {
	mu sync.RWMutex
	m  map[string]muxEntry
}

// muxEntry is a HTTP request multiplexer entry
type muxEntry struct {
	h       Handler
	pattern string
}

// DefaultServeMux is the default [ServeMux] used by [Serve].
var DefaultServeMux = &ServeMux{m: make(map[string]muxEntry)}

func (mux *ServeMux) ServeHTTP(w ResponseWriter, r *Request) {
	h, _ := mux.Handler(r)
	h.ServeHTTP(w, r)
}

// Handler returns the handler to use for the given request, consulting r.Method, r.Host, and r.URL.Path.
// It always returns a non-nil handler.
func (mux *ServeMux) Handler(r *Request) (h Handler, pattern string) {
	mux.mu.RLock()
	defer mux.mu.RUnlock()

	h, pattern = mux.m[r.URL.Path].h, r.URL.Path
	if h == nil {
		h, pattern = NotFoundHandler(), ""
	}
	return
}

// HandleFunc registers the handler for the given pattern.
func (mux *ServeMux) HandleFunc(pattern string, handler func(ResponseWriter, *Request)) {
	mux.Handle(pattern, HandlerFunc(handler))
}

// Handle registers the handler for the given pattern using the default ServeMux.
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
