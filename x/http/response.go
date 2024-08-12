package http

import (
	"io"
)

type Response struct {
	Status        string
	StatusCode    int
	Header        Header
	Body          io.ReadCloser
	ContentLength int64
}
