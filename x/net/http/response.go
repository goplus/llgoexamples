package http

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgoexamples/rust/hyper"
)

type Response struct {
	Status           string // e.g. "200 OK"
	StatusCode       int    // e.g. 200
	Proto            string // e.g. "HTTP/1.0"
	ProtoMajor       int    // e.g. 1
	ProtoMinor       int    // e.g. 0
	Header           Header
	Body             io.ReadCloser
	ContentLength    int64
	TransferEncoding []string
	Close            bool
	Uncompressed     bool
	//Trailer          Header
	Request *Request
}

func (r *Response) closeBody() {
	if r.Body != nil {
		r.Body.Close()
	}
}

// bodyIsWritable reports whether the Body supports writing. The
// Transport returns Writable bodies for 101 Switching Protocols
// responses.
// The Transport uses this method to determine whether a persistent
// connection is done being managed from its perspective. Once we
// return a writable response body to a user, the net/http package is
// done managing that connection.
func (r *Response) bodyIsWritable() bool {
	_, ok := r.Body.(io.Writer)
	return ok
}

// Cookies parses and returns the cookies set in the Set-Cookie headers.
func (r *Response) Cookies() []*Cookie {
	return readSetCookies(r.Header)
}

func (r *Response) checkRespBody(taskData *taskData) (needContinue bool) {
	pc := taskData.pc
	bodyWritable := r.bodyIsWritable()
	hasBody := taskData.req.Method != "HEAD" && r.ContentLength != 0

	if r.Close || taskData.req.Close || r.StatusCode <= 199 || bodyWritable {
		// Don't do keep-alive on error if either party requested a close
		// or we get an unexpected informational (1xx) response.
		// StatusCode 100 is already handled above.
		pc.alive = false
	}

	if !hasBody || bodyWritable {
		replaced := pc.t.replaceReqCanceler(taskData.req.cancelKey, nil)

		// Put the idle conn back into the pool before we send the response
		// so if they process it quickly and make another request, they'll
		// get this same conn. But we use the unbuffered channel 'rc'
		// to guarantee that persistConn.roundTrip got out of its select
		// potentially waiting for this persistConn to close.
		pc.alive = pc.alive &&
			replaced && pc.tryPutIdleConn()

		if bodyWritable {
			pc.closeErr = errCallerOwnsConn
		}

		select {
		case taskData.resc <- responseAndError{res: r}:
		case <-taskData.callerGone:
			if debugSwitch {
				println("############### checkRespBody callerGone")
			}
			closeAndRemoveIdleConn(pc, true)
			return true
		}
		// Now that they've read from the unbuffered channel, they're safely
		// out of the select that also waits on this goroutine to die, so
		// we're allowed to exit now if needed (if alive is false)
		if debugSwitch {
			println("############### checkRespBody return")
		}
		closeAndRemoveIdleConn(pc, false)
		return true
	}
	return false
}

func (r *Response) wrapRespBody(taskData *taskData) {
	body := &bodyEOFSignal{
		body: r.Body,
		earlyCloseFn: func() error {
			// If the response body is closed prematurely,
			// the hyperBody needs to be recycled and the persistConn needs to be handled.
			taskData.closeHyperBody()
			select {
			case <-taskData.pc.closech:
				taskData.pc.t.removeIdleConn(taskData.pc)
			default:
			}
			replaced := taskData.pc.t.replaceReqCanceler(taskData.req.cancelKey, nil) // before pc might return to idle pool
			taskData.pc.alive = taskData.pc.alive &&
				replaced && taskData.pc.tryPutIdleConn()
			return nil
		},
		fn: func(err error) error {
			isEOF := err == io.EOF
			if !isEOF {
				if cerr := taskData.pc.canceled(); cerr != nil {
					return cerr
				}
			}
			return err
		},
	}
	r.Body = body
	// TODO(hah) gzip(wrapRespBody): The compress/gzip library still has a bug. An exception occurs when calling gzip.NewReader().
	//if taskData.addedGzip && EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
	//	println("gzip reader")
	//	r.Body = &gzipReader{body: body}
	//	r.Header.Del("Content-Encoding")
	//	r.Header.Del("Content-Length")
	//	r.ContentLength = -1
	//	r.Uncompressed = true
	//}
}

// bodyEOFSignal is used by the HTTP/1 transport when reading response
// bodies to make sure we see the end of a response body before
// proceeding and reading on the connection again.
//
// It wraps a ReadCloser but runs fn (if non-nil) at most
// once, right before its final (error-producing) Read or Close call
// returns. fn should return the new error to return from Read or Close.
//
// If earlyCloseFn is non-nil and Close is called before io.EOF is
// seen, earlyCloseFn is called instead of fn, and its return value is
// the return value from Close.
type bodyEOFSignal struct {
	body         io.ReadCloser
	mu           sync.Mutex        // guards following 4 fields
	closed       bool              // whether Close has been called
	rerr         error             // sticky Read error
	fn           func(error) error // err will be nil on Read io.EOF
	earlyCloseFn func() error      // optional alt Close func used if io.EOF not seen
}

var errReadOnClosedResBody = errors.New("http: read on closed response body")

func (es *bodyEOFSignal) Read(p []byte) (n int, err error) {
	es.mu.Lock()
	closed, rerr := es.closed, es.rerr
	es.mu.Unlock()
	if closed {
		return 0, errReadOnClosedResBody
	}
	if rerr != nil {
		return 0, rerr
	}

	n, err = es.body.Read(p)
	if err != nil {
		es.mu.Lock()
		defer es.mu.Unlock()
		if es.rerr == nil {
			es.rerr = err
		}
		err = es.condfn(err)
	}
	return
}

func (es *bodyEOFSignal) Close() error {
	es.mu.Lock()
	defer es.mu.Unlock()
	if es.closed {
		return nil
	}
	es.closed = true
	if es.earlyCloseFn != nil && es.rerr != io.EOF {
		return es.earlyCloseFn()
	}
	err := es.body.Close()
	return es.condfn(err)
}

// caller must hold es.mu.
func (es *bodyEOFSignal) condfn(err error) error {
	if es.fn == nil {
		return err
	}
	err = es.fn(err)
	es.fn = nil
	return err
}

// gzipReader wraps a response body so it can lazily
// call gzip.NewReader on the first call to Read
type gzipReader struct {
	_    incomparable
	body *bodyEOFSignal // underlying HTTP/1 response body framing
	zr   *gzip.Reader   // lazily-initialized gzip reader
	zerr error          // any error from gzip.NewReader; sticky
}

func (gz *gzipReader) Read(p []byte) (n int, err error) {
	if gz.zr == nil {
		if gz.zerr == nil {
			gz.zr, gz.zerr = gzip.NewReader(gz.body)
		}
		if gz.zerr != nil {
			return 0, gz.zerr
		}
	}

	gz.body.mu.Lock()
	if gz.body.closed {
		err = errReadOnClosedResBody
	}
	gz.body.mu.Unlock()

	if err != nil {
		return 0, err
	}
	return gz.zr.Read(p)
}

func (gz *gzipReader) Close() error {
	return gz.body.Close()
}

func ReadResponse(r io.ReadCloser, req *Request, hyperResp *hyper.Response) (*Response, error) {
	resp := &Response{
		Request: req,
		Header:  make(Header),
		//Trailer: make(Header),
	}
	readResponseLineAndHeader(resp, hyperResp)

	fixPragmaCacheControl(req.Header)

	err := readTransfer(resp, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// readResponseLineAndHeader reads the response line and header from hyper response.
func readResponseLineAndHeader(resp *Response, hyperResp *hyper.Response) {
	rp := hyperResp.ReasonPhrase()
	rpLen := hyperResp.ReasonPhraseLen()

	// Parse the first line of the response.
	resp.Status = strconv.Itoa(int(hyperResp.Status())) + " " + c.GoString((*int8)(c.Pointer(rp)), rpLen)
	resp.StatusCode = int(hyperResp.Status())
	version := int(hyperResp.Version())
	resp.ProtoMajor, resp.ProtoMinor = splitTwoDigitNumber(version)
	resp.Proto = fmt.Sprintf("HTTP/%d.%d", resp.ProtoMajor, resp.ProtoMinor)

	headers := hyperResp.Headers()
	headers.Foreach(appendToResponseHeader, c.Pointer(resp))
}

// RFC 7234, section 5.4: Should treat
//
//	Pragma: no-cache
//
// like
//
//	Cache-Control: no-cache
func fixPragmaCacheControl(header Header) {
	if hp, ok := header["Pragma"]; ok && len(hp) > 0 && hp[0] == "no-cache" {
		if _, presentcc := header["Cache-Control"]; !presentcc {
			header["Cache-Control"] = []string{"no-cache"}
		}
	}
}

// isProtocolSwitchHeader reports whether the request or response header
// is for a protocol switch.
func isProtocolSwitchHeader(h Header) bool {
	return h.Get("Upgrade") != "" &&
		HeaderValuesContainsToken(h["Connection"], "Upgrade")
}
