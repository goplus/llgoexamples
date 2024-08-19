package http

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/idna"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/os"
	"github.com/goplus/llgoexamples/rust/hyper"
)

type Request struct {
	Method           string
	URL              *url.URL
	Proto            string // "HTTP/1.0"
	ProtoMajor       int    // 1
	ProtoMinor       int    // 0
	Header           Header
	Body             io.ReadCloser
	GetBody          func() (io.ReadCloser, error)
	ContentLength    int64
	TransferEncoding []string
	Close            bool
	Host             string
	//Form             url.Values
	//PostForm         url.Values
	//MultipartForm    *multipart.Form
	Trailer    Header
	RemoteAddr string
	RequestURI string
	//TLS              *tls.ConnectionState
	Cancel   <-chan struct{}
	Response *Response
	timeout  time.Duration
	ctx      context.Context
}

var defaultChunkSize uintptr = 8192

// NewRequest wraps NewRequestWithContext using context.Background.
func NewRequest(method, url string, body io.Reader) (*Request, error) {
	return NewRequestWithContext(context.Background(), method, url, body)
}

// NewRequestWithContext returns a new Request given a method, URL, and
// optional body.
//
// If the provided body is also an io.Closer, the returned
// Request.Body is set to body and will be closed by the Client
// methods Do, Post, and PostForm, and Transport.RoundTrip.
//
// NewRequestWithContext returns a Request suitable for use with
// Client.Do or Transport.RoundTrip. To create a request for use with
// testing a Server Handler, either use the NewRequest function in the
// net/http/httptest package, use ReadRequest, or manually update the
// Request fields. For an outgoing client request, the context
// controls the entire lifetime of a request and its response:
// obtaining a connection, sending the request, and reading the
// response headers and body. See the Request type's documentation for
// the difference between inbound and outbound request fields.
//
// If body is of type *bytes.Buffer, *bytes.Reader, or
// *strings.Reader, the returned request's ContentLength is set to its
// exact value (instead of -1), GetBody is populated (so 307 and 308
// redirects can replay the body), and Body is set to NoBody if the
// ContentLength is 0.
func NewRequestWithContext(ctx context.Context, method, urlStr string, body io.Reader) (*Request, error) {
	if method == "" {
		// We document that "" means "GET" for Request.Method, and people have
		// relied on that from NewRequest, so keep that working.
		// We still enforce validMethod for non-empty methods.
		method = "GET"
	}
	if !validMethod(method) {
		return nil, fmt.Errorf("net/http: invalid method %q", method)
	}
	//if ctx == nil {
	//	return nil, errors.New("net/http: nil Context")
	//}
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	rc, ok := body.(io.ReadCloser)
	if !ok && body != nil {
		rc = io.NopCloser(body)
	}
	// The host's colon:port should be normalized. See Issue 14836.
	u.Host = removeEmptyPort(u.Host)
	req := &Request{
		ctx:        ctx,
		Method:     method,
		URL:        u,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(Header),
		Body:       rc,
		Host:       u.Host,
	}
	if body != nil {
		switch v := body.(type) {
		case *bytes.Buffer:
			req.ContentLength = int64(v.Len())
			buf := v.Bytes()
			req.GetBody = func() (io.ReadCloser, error) {
				r := bytes.NewReader(buf)
				return io.NopCloser(r), nil
			}
		case *bytes.Reader:
			req.ContentLength = int64(v.Len())
			snapshot := *v
			req.GetBody = func() (io.ReadCloser, error) {
				r := snapshot
				return io.NopCloser(&r), nil
			}
		case *strings.Reader:
			req.ContentLength = int64(v.Len())
			snapshot := *v
			req.GetBody = func() (io.ReadCloser, error) {
				r := snapshot
				return io.NopCloser(&r), nil
			}
		default:
			// This is where we'd set it to -1 (at least
			// if body != NoBody) to mean unknown, but
			// that broke people during the Go 1.8 testing
			// period. People depend on it being 0 I
			// guess. Maybe retry later. See Issue 18117.
		}
		// For client requests, Request.ContentLength of 0
		// means either actually 0, or unknown. The only way
		// to explicitly say that the ContentLength is zero is
		// to set the Body to nil. But turns out too much code
		// depends on NewRequest returning a non-nil Body,
		// so we use a well-known ReadCloser variable instead
		// and have the http package also treat that sentinel
		// variable to mean explicitly zero.
		if req.GetBody != nil && req.ContentLength == 0 {
			req.Body = NoBody
			req.GetBody = func() (io.ReadCloser, error) { return NoBody, nil }
		}
	}

	return req, nil
}

func printInformational(userdata c.Pointer, resp *hyper.Response) {
	status := resp.Status()
	fmt.Println("Informational (1xx): ", status)
}

type postReq struct {
	req *Request
	buf []byte
}

func setPostData(userdata c.Pointer, ctx *hyper.Context, chunk **hyper.Buf) c.Int {
	req := (*postReq)(userdata)
	n, err := req.req.Body.Read(req.buf)
	if err != nil {
		if err == io.EOF {
			*chunk = nil
			return hyper.PollReady
		}
		fmt.Println("error reading upload file: ", err)
		return hyper.PollError
	}
	if n > 0 {
		*chunk = hyper.CopyBuf(&req.buf[0], uintptr(n))
		return hyper.PollReady
	}
	if n == 0 {
		*chunk = nil
		return hyper.PollReady
	}

	fmt.Printf("error reading request body: %s\n", c.GoString(c.Strerror(os.Errno)))
	return hyper.PollError
}

func setPostDataNoCopy(userdata c.Pointer, ctx *hyper.Context, chunk **hyper.Buf) c.Int {
	type buf struct {
		data   *uint8
		len    uintptr
		Unused [16]byte
	}
	req := (*postReq)(userdata)
	buffer := &buf{
		data: &req.buf[0],
		len:  uintptr(len(req.buf)),
	}

	*chunk = (*hyper.Buf)(c.Pointer(buffer))
	n, err := req.req.Body.Read(req.buf)
	if err != nil {
		if err == io.EOF {
			*chunk = nil
			return hyper.PollReady
		}
		fmt.Println("error reading upload file: ", err)
		return hyper.PollError
	}
	if n > 0 {
		return hyper.PollReady
	}
	if n == 0 {
		*chunk = nil
		return hyper.PollReady
	}

	fmt.Printf("error reading request body: %s\n", c.GoString(c.Strerror(os.Errno)))
	return hyper.PollError
}

func newHyperRequest(req *Request) (*hyper.Request, error) {
	host := req.Host
	uri := req.URL.RequestURI()
	method := req.Method
	// Prepare the request
	hyperReq := hyper.NewRequest()
	// Set the request method and uri
	if hyperReq.SetMethod(&[]byte(method)[0], c.Strlen(c.AllocaCStr(method))) != hyper.OK {
		return nil, fmt.Errorf("error setting method %s\n", method)
	}
	if hyperReq.SetURI(&[]byte(uri)[0], c.Strlen(c.AllocaCStr(uri))) != hyper.OK {
		return nil, fmt.Errorf("error setting uri %s\n", uri)
	}
	// Set the request headers
	reqHeaders := hyperReq.Headers()
	if reqHeaders.Set(&[]byte("Host")[0], c.Strlen(c.Str("Host")), &[]byte(host)[0], c.Strlen(c.AllocaCStr(host))) != hyper.OK {
		return nil, fmt.Errorf("error setting header: Host: %s\n", host)
	}

	if method == "POST" && req.Body != nil {
		req.Header.Set("expect", "100-continue")
		hyperReq.OnInformational(printInformational, nil)

		hyperReqBody := hyper.NewBody()
		reqData := &postReq{
			req: req,
			buf: make([]byte, 3),
		}
		hyperReqBody.SetUserdata(c.Pointer(reqData))
		hyperReqBody.SetDataFunc(setPostData)
		hyperReq.SetBody(hyperReqBody)
	}

	// Add user-defined request headers to hyper.Request
	err := req.setHeaders(hyperReq)
	if err != nil {
		return nil, err
	}

	return hyperReq, nil
}

// setHeaders sets the headers of the request
func (req *Request) setHeaders(hyperReq *hyper.Request) error {
	headers := hyperReq.Headers()
	for key, values := range req.Header {
		valueLen := len(values)
		if valueLen > 1 {
			for _, value := range values {
				if headers.Add(&[]byte(key)[0], c.Strlen(c.AllocaCStr(key)), &[]byte(value)[0], c.Strlen(c.AllocaCStr(value))) != hyper.OK {
					return fmt.Errorf("error adding header %s: %s\n", key, value)
				}
			}
		} else if valueLen == 1 {
			if headers.Set(&[]byte(key)[0], c.Strlen(c.AllocaCStr(key)), &[]byte(values[0])[0], c.Strlen(c.AllocaCStr(values[0]))) != hyper.OK {
				return fmt.Errorf("error setting header %s: %s\n", key, values[0])
			}
		} else {
			return fmt.Errorf("error setting header %s: empty value\n", key)
		}
	}
	return nil
}

func (r *Request) closeBody() error {
	if r.Body == nil {
		return nil
	}
	return r.Body.Close()
}

func validMethod(method string) bool {
	/*
	     Method         = "OPTIONS"                ; Section 9.2
	                    | "GET"                    ; Section 9.3
	                    | "HEAD"                   ; Section 9.4
	                    | "POST"                   ; Section 9.5
	                    | "PUT"                    ; Section 9.6
	                    | "DELETE"                 ; Section 9.7
	                    | "TRACE"                  ; Section 9.8
	                    | "CONNECT"                ; Section 9.9
	                    | extension-method
	   extension-method = token
	     token          = 1*<any CHAR except CTLs or separators>
	*/
	return len(method) > 0 && strings.IndexFunc(method, isNotToken) == -1
}

// Context returns the request's context. To change the context, use
// Clone or WithContext.
//
// The returned context is always non-nil; it defaults to the
// background context.
//
// For outgoing client requests, the context controls cancellation.
//
// For incoming server requests, the context is canceled when the
// client's connection closes, the request is canceled (with HTTP/2),
// or when the ServeHTTP method returns.
func (r *Request) Context() context.Context {
	if r.ctx != nil {
		return r.ctx
	}
	return context.Background()
}

// AddCookie adds a cookie to the request. Per RFC 6265 section 5.4,
// AddCookie does not attach more than one Cookie header field. That
// means all cookies, if any, are written into the same line,
// separated by semicolon.
// AddCookie only sanitizes c's name and value, and does not sanitize
// a Cookie header already present in the request.
func (r *Request) AddCookie(c *Cookie) {
	s := fmt.Sprintf("%s=%s", sanitizeCookieName(c.Name), sanitizeCookieValue(c.Value))
	if c := r.Header.Get("Cookie"); c != "" {
		r.Header.Set("Cookie", c+"; "+s)
	} else {
		r.Header.Set("Cookie", s)
	}
}

// requiresHTTP1 reports whether this request requires being sent on
// an HTTP/1 connection.
func (r *Request) requiresHTTP1() bool {
	return hasToken(r.Header.Get("Connection"), "upgrade") &&
		EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// Cookies parses and returns the HTTP cookies sent with the request.
func (r *Request) Cookies() []*Cookie {
	return readCookies(r.Header, "")
}

// readCookies parses all "Cookie" values from the header h and
// returns the successfully parsed Cookies.
//
// if filter isn't empty, only cookies of that name are returned.
func readCookies(h Header, filter string) []*Cookie {
	lines := h["Cookie"]
	if len(lines) == 0 {
		return []*Cookie{}
	}

	cookies := make([]*Cookie, 0, len(lines)+strings.Count(lines[0], ";"))
	for _, line := range lines {
		line = textproto.TrimString(line)

		var part string
		for len(line) > 0 { // continue since we have rest
			part, line, _ = strings.Cut(line, ";")
			part = textproto.TrimString(part)
			if part == "" {
				continue
			}
			name, val, _ := strings.Cut(part, "=")
			name = textproto.TrimString(name)
			if !isCookieNameValid(name) {
				continue
			}
			if filter != "" && filter != name {
				continue
			}
			val, ok := parseCookieValue(val, true)
			if !ok {
				continue
			}
			cookies = append(cookies, &Cookie{Name: name, Value: val})
		}
	}
	return cookies
}

func idnaASCII(v string) (string, error) {
	// TODO: Consider removing this check after verifying performance is okay.
	// Right now punycode verification, length checks, context checks, and the
	// permissible character tests are all omitted. It also prevents the ToASCII
	// call from salvaging an invalid IDN, when possible. As a result it may be
	// possible to have two IDNs that appear identical to the user where the
	// ASCII-only version causes an error downstream whereas the non-ASCII
	// version does not.
	// Note that for correct ASCII IDNs ToASCII will only do considerably more
	// work, but it will not cause an allocation.
	if Is(v) {
		return v, nil
	}
	return idna.Lookup.ToASCII(v)
}
