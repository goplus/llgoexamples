package response_stream

import (
	"errors"
	"io"
	"sync"
)

type onceError struct {
	sync.Mutex
	err error
}

func (a *onceError) Store(err error) {
	a.Lock()
	defer a.Unlock()
	if a.err != nil {
		return
	}
	a.err = err
}

func (a *onceError) Load() error {
	a.Lock()
	defer a.Unlock()
	return a.err
}

var ErrClosedResponseStream = errors.New("response stream: read/write on closed stream")

type responseStream struct {
	wrMu     sync.Mutex
	wrCh     chan []byte
	rdCh     chan struct{}
	rdRemain []byte
	done     chan struct{}
	once     sync.Once
	rerr     onceError
	werr     onceError
}

func (rs *responseStream) write(p []byte) (n int, err error) {
	select {
	case <-rs.done:
		return 0, rs.writeCloseError()
	default:
		rs.wrMu.Lock()
		defer rs.wrMu.Unlock()
	}

	rs.wrCh <- p
	return len(p), nil
}

func (rs *responseStream) read(p []byte) (n int, err error) {
	if len(rs.rdRemain) > 0 {
		n = copy(p, rs.rdRemain)
		rs.rdRemain = rs.rdRemain[n:]
		if len(rs.rdRemain) == 0 {
			select {
			case rs.rdCh <- struct{}{}:
			default:
			}
		}
		return n, nil
	}

	select {
	case <-rs.done:
		return 0, rs.rerr.Load()
	case data := <-rs.wrCh:
		n = copy(p, data)
		if n < len(data) {
			rs.rdRemain = data[n:]
		} else {
			select {
			case rs.rdCh <- struct{}{}:
			default:
			}
		}
		return n, nil
	}
}

func (rs *responseStream) closeWrite(err error) error {
	if err == nil {
		err = io.EOF
	}
	rs.werr.Store(err)
	rs.once.Do(func() {
		close(rs.done)
	})
	return nil
}

func (rs *responseStream) closeRead(err error) error {
	if err == nil {
		err = ErrClosedResponseStream
	}
	rs.rerr.Store(err)
	rs.once.Do(func() {
		close(rs.done)
	})
	return nil
}

func (rs *responseStream) Close() error {
	rs.once.Do(func() {
		close(rs.done)
	})
	return rs.readCloseError()
}

func (rs *responseStream) readCloseError() error {
	werr := rs.werr.Load()
	if rerr := rs.rerr.Load(); werr == nil && rerr != nil {
		return rerr
	}
	return ErrClosedResponseStream
}

func (rs *responseStream) writeCloseError() error {
	werr := rs.werr.Load()
	if werr != nil {
		return werr
	}
	return ErrClosedResponseStream
}


type ResponseStreamReader struct {
	responseStream
}

func (rs *ResponseStreamReader) Read(data []byte) (n int, err error) {
	return rs.responseStream.read(data)
}

func (rs *ResponseStreamReader) Close() error {
	return rs.closeWithError(nil)
}

func (rs *ResponseStreamReader) closeWithError(err error) error {
	return rs.responseStream.closeRead(err)
}

type ResponseStreamWriter struct {
	r ResponseStreamReader
}

func (rs *ResponseStreamWriter) Write(data []byte) (n int, err error) {
	return rs.r.responseStream.write(data)
}

func (rs *ResponseStreamWriter) Close() error {
	return rs.r.closeWithError(nil)
}

func (rs *ResponseStreamWriter) closeWithError(err error) error {
	return rs.r.responseStream.closeWrite(err)
}

func (rs *ResponseStreamWriter) GetRdCh() <-chan struct{} {
	return rs.r.rdCh
}

func NewResponseStream() (*ResponseStreamReader, *ResponseStreamWriter) {
	rsw := &ResponseStreamWriter{
		r: ResponseStreamReader{
			responseStream: responseStream{
				wrCh: make(chan []byte, 1),
				rdCh: make(chan struct{}, 1),
				done: make(chan struct{}),
			},
		},
	}
	return &rsw.r, rsw
}