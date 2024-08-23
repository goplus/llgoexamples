package http

import (
	"bytes"
	"context"
	"errors"
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
	Cancel    <-chan struct{}
	timeoutch chan struct{} //optional

	Response *Response
	timeout  time.Duration
	ctx      context.Context
}

const defaultChunkSize = 8192

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
	// TODO(spongehah) Hyper only supports http
	isHttpPrefix := strings.HasPrefix(urlStr, "http://")
	isHttpsPrefix := strings.HasPrefix(urlStr, "https://")
	if !isHttpPrefix && !isHttpsPrefix {
		urlStr = "http://" + urlStr
	}
	if isHttpsPrefix {
		urlStr = "http://" + strings.TrimPrefix(urlStr, "https://")
	}

	if method == "" {
		// We document that "" means "GET" for Request.Method, and people have
		// relied on that from NewRequest, so keep that working.
		// We still enforce validMethod for non-empty methods.
		method = "GET"
	}
	if !validMethod(method) {
		return nil, fmt.Errorf("net/http: invalid method %q", method)
	}
	if ctx == nil {
		return nil, errors.New("net/http: nil Context")
	}
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

//func setPostDataNoCopy(userdata c.Pointer, ctx *hyper.Context, chunk **hyper.Buf) c.Int {
//	req := (*postReq)(userdata)
//	buf := req.hyperBuf.Bytes()
//	len := req.hyperBuf.Len()
//	n, err := req.req.Body.Read(unsafe.Slice(buf, len))
//	if err != nil {
//		if err == io.EOF {
//			*chunk = nil
//			return hyper.PollReady
//		}
//		fmt.Println("error reading upload file: ", err)
//		return hyper.PollError
//	}
//	if n > 0 {
//		*chunk = req.hyperBuf
//		return hyper.PollReady
//	}
//	if n == 0 {
//		*chunk = nil
//		return hyper.PollReady
//	}
//
//	fmt.Printf("error reading request body: %s\n", c.GoString(c.Strerror(os.Errno)))
//	return hyper.PollError
//}

// setHeaders sets the headers of the request
func (r *Request) setHeaders(hyperReq *hyper.Request) error {
	headers := hyperReq.Headers()
	for key, values := range r.Header {
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

func (r *Request) expectsContinue() bool {
	return hasToken(r.Header.get("Expect"), "100-continue")
}

func (r *Request) wantsClose() bool {
	if r.Close {
		return true
	}
	return hasToken(r.Header.get("Connection"), "close")
}

func (r *Request) closeBody() error {
	if r.Body == nil {
		return nil
	}
	return r.Body.Close()
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

// ProtoAtLeast reports whether the HTTP protocol used
// in the request is at least major.minor.
func (r *Request) ProtoAtLeast(major, minor int) bool {
	return r.ProtoMajor > major ||
		r.ProtoMajor == major && r.ProtoMinor >= minor
}

// errMissingHost is returned by Write when there is no Host or URL present in
// the Request.
var errMissingHost = errors.New("http: Request.Write on Request with no Host or URL set")

// extraHeaders may be nil
// waitForContinue may be nil
// always closes body
func (r *Request) write(usingProxy bool, extraHeader Header, client *hyper.ClientConn, exec *hyper.Executor) (err error) {
	//trace := httptrace.ContextClientTrace(r.Context())
	//if trace != nil && trace.WroteRequest != nil {
	//	defer func() {
	//		trace.WroteRequest(httptrace.WroteRequestInfo{
	//			Err: err,
	//		})
	//	}()
	//}

	//closed := false
	//defer func() {
	//	if closed {
	//		return
	//	}
	//	if closeErr := r.closeBody(); closeErr != nil && err == nil {
	//		err = closeErr
	//	}
	//}()

	// Prepare the hyper.Request
	hyperReq, err := r.newHyperRequest(usingProxy, extraHeader)
	if err != nil {
		return err
	}
	// Send it!
	sendTask := client.Send(hyperReq)
	setTaskId(sendTask, read)
	sendRes := exec.Push(sendTask)
	if sendRes != hyper.OK {
		err = errors.New("failed to send the request")
	}
	return err
}

func (r *Request) newHyperRequest(usingProxy bool, extraHeader Header) (*hyper.Request, error) {
	// Find the target host. Prefer the Host: header, but if that
	// is not given, use the host from the request URL.
	//
	// Clean the host, in case it arrives with unexpected stuff in it.
	host := r.Host
	if host == "" {
		if r.URL == nil {
			return nil, errMissingHost
		}
		host = r.URL.Host
	}
	host, err := PunycodeHostPort(host)
	if err != nil {
		return nil, err
	}
	// Validate that the Host header is a valid header in general,
	// but don't validate the host itself. This is sufficient to avoid
	// header or request smuggling via the Host field.
	// The server can (and will, if it's a net/http server) reject
	// the request if it doesn't consider the host valid.
	if !ValidHostHeader(host) {
		// Historically, we would truncate the Host header after '/' or ' '.
		// Some users have relied on this truncation to convert a network
		// address such as Unix domain socket path into a valid, ignored
		// Host header (see https://go.dev/issue/61431).
		//
		// We don't preserve the truncation, because sending an altered
		// header field opens a smuggling vector. Instead, zero out the
		// Host header entirely if it isn't valid. (An empty Host is valid;
		// see RFC 9112 Section 3.2.)
		//
		// Return an error if we're sending to a proxy, since the proxy
		// probably can't do anything useful with an empty Host header.
		if !usingProxy {
			host = ""
		} else {
			return nil, errors.New("http: invalid Host header")
		}
	}

	// According to RFC 6874, an HTTP client, proxy, or other
	// intermediary must remove any IPv6 zone identifier attached
	// to an outgoing URI.
	host = removeZone(host)

	ruri := r.URL.RequestURI()
	if usingProxy && r.URL.Scheme != "" && r.URL.Opaque == "" {
		ruri = r.URL.Scheme + "://" + host + ruri
	} else if r.Method == "CONNECT" && r.URL.Path == "" {
		// CONNECT requests normally give just the host and port, not a full URL.
		ruri = host
		if r.URL.Opaque != "" {
			ruri = r.URL.Opaque
		}
	}
	if stringContainsCTLByte(ruri) {
		return nil, errors.New("net/http: can't write control character in Request.URL")
	}




	// Prepare the hyper request
	hyperReq := hyper.NewRequest()

	// Set the request line, default HTTP/1.1
	if hyperReq.SetMethod(&[]byte(r.Method)[0], c.Strlen(c.AllocaCStr(r.Method))) != hyper.OK {
		return nil, fmt.Errorf("error setting method %s\n", r.Method)
	}
	if hyperReq.SetURI(&[]byte(ruri)[0], c.Strlen(c.AllocaCStr(ruri))) != hyper.OK {
		return nil, fmt.Errorf("error setting uri %s\n", ruri)
	}
	if hyperReq.SetVersion(c.Int(hyper.HTTPVersion11)) != hyper.OK {
		return nil, fmt.Errorf("error setting httpversion %s\n", "HTTP/1.1")
	}

	// Set the request headers
	reqHeaders := hyperReq.Headers()
	if reqHeaders.Set(&[]byte("Host")[0], c.Strlen(c.Str("Host")), &[]byte(host)[0], c.Strlen(c.AllocaCStr(host))) != hyper.OK {
		return nil, fmt.Errorf("error setting header: Host: %s\n", host)
	}
	err = r.setHeaders(hyperReq)
	if err != nil {
		return nil, err
	}

	if r.Body != nil {
		// 100-continue
		if r.ProtoAtLeast(1, 1) && r.Body != nil && r.expectsContinue() {
			hyperReq.OnInformational(printInformational, nil)
		}

		hyperReqBody := hyper.NewBody()
		//buf := make([]byte, 2)
		//hyperBuf := hyper.CopyBuf(&buf[0], uintptr(2))
		reqData := &postReq{
			req: r,
			buf: make([]byte, defaultChunkSize),
			//hyperBuf: hyperBuf,
		}
		hyperReqBody.SetUserdata(c.Pointer(reqData))
		hyperReqBody.SetDataFunc(setPostData)
		//hyperReqBody.SetDataFunc(setPostDataNoCopy)
		hyperReq.SetBody(hyperReqBody)
	}

	return hyperReq, nil
}

func printInformational(userdata c.Pointer, resp *hyper.Response) {
	status := resp.Status()
	fmt.Println("Informational (1xx): ", status)
}

type postReq struct {
	req *Request
	buf []byte
	//hyperBuf *hyper.Buf
}

func setPostData(userdata c.Pointer, ctx *hyper.Context, chunk **hyper.Buf) c.Int {
	req := (*postReq)(userdata)
	n, err := req.req.Body.Read(req.buf)
	if err != nil {
		if err == io.EOF {
			println("EOF")
			*chunk = nil
			req.req.Body.Close()
			return hyper.PollReady
		}
		fmt.Println("error reading request body: ", err)
		return hyper.PollError
	}
	if n > 0 {
		*chunk = hyper.CopyBuf(&req.buf[0], uintptr(n))
		return hyper.PollReady
	}
	if n == 0 {
		println("n == 0")
		*chunk = nil
		req.req.Body.Close()
		return hyper.PollReady
	}

	req.req.Body.Close()
	fmt.Printf("error reading request body: %s\n", c.GoString(c.Strerror(os.Errno)))
	return hyper.PollError
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

// requestBodyReadError wraps an error from (*Request).write to indicate
// that the error came from a Read call on the Request.Body.
// This error type should not escape the net/http package to users.
type requestBodyReadError struct{ error }

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

// removeZone removes IPv6 zone identifier from host.
// E.g., "[fe80::1%en0]:8080" to "[fe80::1]:8080"
func removeZone(host string) string {
	if !strings.HasPrefix(host, "[") {
		return host
	}
	i := strings.LastIndex(host, "]")
	if i < 0 {
		return host
	}
	j := strings.LastIndex(host[:i], "%")
	if j < 0 {
		return host
	}
	return host[:j] + host[i:]
}
