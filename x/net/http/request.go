package http

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"net/url"
	"strings"
	"time"
	"unsafe"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgo/c/libuv"
	"github.com/goplus/llgoexamples/rust/hyper"
	"golang.org/x/net/idna"
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
	// Form             url.Values
	// PostForm         url.Values
	// MultipartForm    *multipart.Form
	RemoteAddr string
	RequestURI string

	Response *Response

	deadline  time.Time
	timeoutch chan struct{}
	timer     *libuv.Timer
}

const defaultChunkSize = 8192

// NOTE: This is not intended to reflect the actual Go version being used.
// It was changed at the time of Go 1.1 release because the former User-Agent
// had ended up blocked by some intrusion detection systems.
// See https://codereview.appspot.com/7532043.
const defaultUserAgent = "Go-http-client/1.1"

// errMissingHost is returned by Write when there is no Host or URL present in
// the Request.
var errMissingHost = errors.New("http: Request.Write on Request with no Host or URL set")

// Headers that Request.Write handles itself and should be skipped.
var reqWriteExcludeHeader = map[string]bool{
	"Host":              true, // not in Header map anyway
	"User-Agent":        true,
	"Content-Length":    true,
	"Transfer-Encoding": true,
	"Trailer":           true,
}

// requestBodyReadError wraps an error from (*Request).write to indicate
// that the error came from a Read call on the Request.Body.
// This error type should not escape the net/http package to users.
type requestBodyReadError struct{ error }

// NewRequest wraps NewRequestWithContext using context.Background.
func NewRequest(method, urlStr string, body io.Reader) (*Request, error) {
	// TODO(hah) Hyper only supports http
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
		Method:     method,
		URL:        u,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(Header),
		Body:       rc,
		Host:       u.Host,
		timer:      nil,
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

func (r *Request) isReplayable() bool {
	if r.Body == nil || r.Body == NoBody || r.GetBody != nil {
		switch valueOrDefault(r.Method, "GET") {
		case "GET", "HEAD", "OPTIONS", "TRACE":
			return true
		}
		// The Idempotency-Key, while non-standard, is widely used to
		// mean a POST or other request is idempotent. See
		// https://golang.org/issue/19943#issuecomment-421092421
		if r.Header.has("Idempotency-Key") || r.Header.has("X-Idempotency-Key") {
			return true
		}
	}
	return false
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

// extraHeaders may be nil
// waitForContinue may be nil
// always closes body
func (r *Request) write(client *hyper.ClientConn, taskData *clientTaskData, exec *hyper.Executor) (err error) {
	//trace := httptrace.ContextClientTrace(r.Context())
	//if trace != nil && trace.WroteRequest != nil {
	//	defer func() {
	//		trace.WroteRequest(httptrace.WroteRequestInfo{
	//			Err: err,
	//		})
	//	}()
	//}

	// Prepare the hyper.Request
	hyperReq, err := r.newHyperRequest(taskData.pc.isProxy, taskData.req.extra, taskData.req)
	if err != nil {
		return err
	}
	// Send it!
	sendTask := client.Send(hyperReq)
	if sendTask == nil {
		println("############### write: sendTask is nil")
		return errors.New("failed to send the request")
	}
	sendTask.SetUserdata(c.Pointer(taskData), nil)
	sendRes := exec.Push(sendTask)
	if sendRes != hyper.OK {
		err = errors.New("failed to send the request")
	}
	return err
}

func (r *Request) newHyperRequest(usingProxy bool, extraHeader Header, treq *transportRequest) (*hyper.Request, error) {
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

	// Use the defaultUserAgent unless the Header contains one, which
	// may be blank to not send the header.
	userAgent := defaultUserAgent
	if r.Header.has("User-Agent") {
		userAgent = r.Header.Get("User-Agent")
	}
	if userAgent != "" {
		if reqHeaders.Set(&[]byte("User-Agent")[0], c.Strlen(c.Str("User-Agent")), &[]byte(userAgent)[0], c.Strlen(c.AllocaCStr(userAgent))) != hyper.OK {
			return nil, fmt.Errorf("error setting header: User-Agent: %s\n", userAgent)
		}
	}

	// Process Body,ContentLength,Close,Trailer
	err = r.writeHeader(reqHeaders)
	if err != nil {
		return nil, err
	}

	err = r.Header.writeSubset(reqHeaders, reqWriteExcludeHeader)
	if err != nil {
		return nil, err
	}

	if extraHeader != nil {
		err = extraHeader.write(reqHeaders)
		if err != nil {
			return nil, err
		}
	}

	//if trace != nil && trace.WroteHeaders != nil {
	//	trace.WroteHeaders()
	//}

	// Wait for 100-continue if expected.
	if r.ProtoAtLeast(1, 1) && r.Body != nil && r.expectsContinue() {
		hyperReq.OnInformational(printInformational, nil, nil)
	}

	// Write body and trailer
	err = r.writeBody(hyperReq, treq)
	if err != nil {
		return nil, err
	}

	return hyperReq, nil
}

func printInformational(userdata c.Pointer, resp *hyper.Response) {
	status := resp.Status()
	fmt.Println("Informational (1xx): ", status)
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

// Return value if nonempty, def otherwise.
func valueOrDefault(value, def string) string {
	if value != "" {
		return value
	}
	return def
}

// readRequest reads the request from the hyper request
func readRequest(executor *hyper.Executor, hyperReq *hyper.Request, requestNotifyHandle *libuv.Async, remoteAddr string) (*Request, error) {
	req := Request{
		Header: make(Header),
		Body:   nil,
	}
	req.RemoteAddr = remoteAddr

	//get the request headers
	headers := hyperReq.Headers()
	if headers != nil {
		headers.Foreach(addHeader, unsafe.Pointer(&req))
	} else {
		return nil, fmt.Errorf("failed to get request headers")
	}

	//get the host from the request header
	var host string
	for key, values := range req.Header {
		if strings.EqualFold(key, "Host") {
			if len(values) > 0 {
				host = values[0]
				break
			}
		}

	}

	method := make([]byte, 32)
	methodLen := unsafe.Sizeof(method)
	if err := hyperReq.Method(&method[0], &methodLen); err != hyper.OK {
		return nil, fmt.Errorf("failed to get method: %v", err)
	}

	methodStr := string(method[:methodLen])

	var scheme, authority, pathAndQuery [1024]byte
	schemeLen, authorityLen, pathAndQueryLen := unsafe.Sizeof(scheme), unsafe.Sizeof(authority), unsafe.Sizeof(pathAndQuery)
	uriResult := hyperReq.URIParts(&scheme[0], &schemeLen, &authority[0], &authorityLen, &pathAndQuery[0], &pathAndQueryLen)
	if uriResult != hyper.OK {
		return nil, fmt.Errorf("failed to get URI parts: %v", uriResult)
	}

	var schemeStr, authorityStr, pathAndQueryStr string
	if schemeLen == 0 {
		schemeStr = "http"
	} else {
		schemeStr = string(scheme[:schemeLen])
	}

	//if authority is empty, use the host from the request header
	if authorityLen == 0 {
		authorityStr = host
	} else {
		authorityStr = string(authority[:authorityLen])
	}

	//if path and query is empty, use the path and query from the request header
	if pathAndQueryLen == 0 {
		return nil, fmt.Errorf("failed to get URI path and query: %v", uriResult)
	} else {
		pathAndQueryStr = string(pathAndQuery[:pathAndQueryLen])
	}
	req.Host = authorityStr
	req.Method = methodStr
	req.RequestURI = pathAndQueryStr

	var proto string
	var protoMajor, protoMinor int
	version := hyperReq.Version()
	switch version {
	case hyper.HTTPVersion10:
		proto = "HTTP/1.0"
		protoMajor = 1
		protoMinor = 0
	case hyper.HTTPVersion11:
		proto = "HTTP/1.1"
		protoMajor = 1
		protoMinor = 1
	case hyper.HTTPVersion2:
		proto = "HTTP/2.0"
		protoMajor = 2
		protoMinor = 0
	case hyper.HTTPVersionNone:
		proto = "HTTP/0.0"
		protoMajor = 0
		protoMinor = 0
	default:
		return nil, fmt.Errorf("unknown HTTP version: %d", version)
	}
	req.Proto = proto
	req.ProtoMajor = protoMajor
	req.ProtoMinor = protoMinor

	urlStr := fmt.Sprintf("%s://%s%s", schemeStr, authorityStr, pathAndQueryStr)
	url, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	req.URL = url

	body := hyperReq.Body()
	if body != nil {
		taskFlag := getBodyTask

		bodyStream := newBodyStream(requestNotifyHandle)
		req.Body = bodyStream

		//prepare task data for hyper executor
		taskData := serverTaskData{
			hyperBody:    body,
			responseBody: nil,
			bodyStream:   bodyStream,
			taskFlag:     taskFlag,
			executor:     executor,
		}

		//set task data to the request notify async handle
		requestNotifyHandle.SetData(c.Pointer(&taskData))

	} else {
		return nil, fmt.Errorf("failed to get request body")
	}

	//hyperReq.Free()

	return &req, nil
}

// addHeader callback function to add the header to the request
func addHeader(data unsafe.Pointer, name *byte, nameLen uintptr, value *byte, valueLen uintptr) c.Int {
	req := (*Request)(data)
	key := string(unsafe.Slice(name, nameLen))
	val := string(unsafe.Slice(value, valueLen))
	values := strings.Split(val, ",")
	if len(values) > 1 {
		for _, v := range values {
			req.Header.Add(key, strings.TrimSpace(v))
		}
	} else {
		req.Header.Add(key, val)
	}
	return hyper.IterContinue
}
