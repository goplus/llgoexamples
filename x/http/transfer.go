package http
//
//import (
//	"fmt"
//	"io"
//	"net/textproto"
//	"strconv"
//	"strings"
//
//	"github.com/goplus/llgoexamples/rust/hyper"
//)
//
//type transferReader struct {
//	// Input
//	Header        Header
//	StatusCode    int
//	RequestMethod string
//	ProtoMajor    int
//	ProtoMinor    int
//	// Output
//	Body          io.ReadCloser
//	ContentLength int64
//	Chunked       bool
//	Close         bool
//	Trailer       Header
//}
//
//// unsupportedTEError reports unsupported transfer-encodings.
//type unsupportedTEError struct {
//	err string
//}
//
//func (uste *unsupportedTEError) Error() string {
//	return uste.err
//}
//
//func readTransfer(resp *Response, hyperResp *hyper.Response) (err error) {
//	//// TODO(spongehah) Replace header operations with using the textproto package
//	//lengthSlice := resp.Header["content-length"]
//	//if lengthSlice == nil {
//	//	resp.ContentLength = -1
//	//} else {
//	//	contentLength := resp.Header["content-length"][0]
//	//	length, err := strconv.Atoi(contentLength)
//	//	if err != nil {
//	//		return err
//	//	}
//	//	resp.ContentLength = int64(length)
//	//}
//
//	t := &transferReader{
//		Header:        resp.Header,
//		StatusCode:    resp.StatusCode,
//		RequestMethod: resp.Request.Method,
//		ProtoMajor:    resp.ProtoMajor,
//		ProtoMinor:    resp.ProtoMinor,
//	}
//
//	// Transfer-Encoding: chunked, and overriding Content-Length.
//	if err = t.parseTransferEncoding(); err != nil {
//		return err
//	}
//
//	realLength, err := fixLength(true, t.StatusCode, t.RequestMethod, t.Header, t.Chunked)
//	if err != nil {
//		return err
//	}
//	if t.RequestMethod == "HEAD" {
//		if n, err := parseContentLength(t.Header.get("Content-Length")); err != nil {
//			return err
//		} else {
//			t.ContentLength = n
//		}
//	} else {
//		t.ContentLength = realLength
//	}
//
//	// Trailer
//	t.Trailer, err = fixTrailer(t.Header, t.Chunked)
//
//	// If there is no Content-Length or chunked Transfer-Encoding on a *Response
//	// and the status is not 1xx, 204 or 304, then the body is unbounded.
//	// See RFC 7230, section 3.3.
//	if realLength == -1 && !t.Chunked && bodyAllowedForStatus(t.StatusCode) {
//		// Unbounded body.
//		t.Close = true
//	}
//
//	return nil
//}
//
//// parseTransferEncoding sets t.Chunked based on the Transfer-Encoding header.
//func (t *transferReader) parseTransferEncoding() error {
//	raw, present := t.Header["Transfer-Encoding"]
//	if !present {
//		return nil
//	}
//	delete(t.Header, "Transfer-Encoding")
//
//	// Issue 12785; ignore Transfer-Encoding on HTTP/1.0 requests.
//	if !t.protoAtLeast(1, 1) {
//		return nil
//	}
//
//	// Like nginx, we only support a single Transfer-Encoding header field, and
//	// only if set to "chunked". This is one of the most security sensitive
//	// surfaces in HTTP/1.1 due to the risk of request smuggling, so we keep it
//	// strict and simple.
//	if len(raw) != 1 {
//		return &unsupportedTEError{fmt.Sprintf("too many transfer encodings: %q", raw)}
//	}
//	if !equalFold(raw[0], "chunked") {
//		return &unsupportedTEError{fmt.Sprintf("unsupported transfer encoding: %q", raw[0])}
//	}
//
//	// RFC 7230 3.3.2 says "A sender MUST NOT send a Content-Length header field
//	// in any message that contains a Transfer-Encoding header field."
//	//
//	// but also: "If a message is received with both a Transfer-Encoding and a
//	// Content-Length header field, the Transfer-Encoding overrides the
//	// Content-Length. Such a message might indicate an attempt to perform
//	// request smuggling (Section 9.5) or response splitting (Section 9.4) and
//	// ought to be handled as an error. A sender MUST remove the received
//	// Content-Length field prior to forwarding such a message downstream."
//	//
//	// Reportedly, these appear in the wild.
//	delete(t.Header, "Content-Length")
//
//	t.Chunked = true
//	return nil
//}
//
//func (t *transferReader) protoAtLeast(m, n int) bool {
//	return t.ProtoMajor > m || (t.ProtoMajor == m && t.ProtoMinor >= n)
//}
//
//// equalFold is strings.EqualFold, ASCII only. It reports whether s and t
//// are equal, ASCII-case-insensitively.
//func equalFold(s, t string) bool {
//	if len(s) != len(t) {
//		return false
//	}
//	for i := 0; i < len(s); i++ {
//		if lower(s[i]) != lower(t[i]) {
//			return false
//		}
//	}
//	return true
//}
//
//// Determine the expected body length, using RFC 7230 Section 3.3. This
//// function is not a method, because ultimately it should be shared by
//// ReadResponse and ReadRequest.
//func fixLength(isResponse bool, status int, requestMethod string, header Header, chunked bool) (int64, error) {
//	isRequest := !isResponse
//	contentLens := header["Content-Length"]
//
//	// Hardening against HTTP request smuggling
//	if len(contentLens) > 1 {
//		// Per RFC 7230 Section 3.3.2, prevent multiple
//		// Content-Length headers if they differ in value.
//		// If there are dups of the value, remove the dups.
//		// See Issue 16490.
//		first := textproto.TrimString(contentLens[0])
//		for _, ct := range contentLens[1:] {
//			if first != textproto.TrimString(ct) {
//				return 0, fmt.Errorf("http: message cannot contain multiple Content-Length headers; got %q", contentLens)
//			}
//		}
//
//		// deduplicate Content-Length
//		header.Del("Content-Length")
//		header.Add("Content-Length", first)
//
//		contentLens = header["Content-Length"]
//	}
//
//	// Logic based on response type or status
//	if isResponse && noResponseBodyExpected(requestMethod) {
//		return 0, nil
//	}
//	if status/100 == 1 {
//		return 0, nil
//	}
//	switch status {
//	case 204, 304:
//		return 0, nil
//	}
//
//	// Logic based on Transfer-Encoding
//	if chunked {
//		return -1, nil
//	}
//
//	// Logic based on Content-Length
//	var cl string
//	if len(contentLens) == 1 {
//		cl = textproto.TrimString(contentLens[0])
//	}
//	if cl != "" {
//		n, err := parseContentLength(cl)
//		if err != nil {
//			return -1, err
//		}
//		return n, nil
//	}
//	header.Del("Content-Length")
//
//	if isRequest {
//		// RFC 7230 neither explicitly permits nor forbids an
//		// entity-body on a GET request so we permit one if
//		// declared, but we default to 0 here (not -1 below)
//		// if there's no mention of a body.
//		// Likewise, all other request methods are assumed to have
//		// no body if neither Transfer-Encoding chunked nor a
//		// Content-Length are set.
//		return 0, nil
//	}
//
//	// Body-EOF logic based on other methods (like closing, or chunked coding)
//	return -1, nil
//}
//
//// parseContentLength trims whitespace from s and returns -1 if no value
//// is set, or the value if it's >= 0.
//func parseContentLength(cl string) (int64, error) {
//	cl = textproto.TrimString(cl)
//	if cl == "" {
//		return -1, nil
//	}
//	n, err := strconv.ParseUint(cl, 10, 63)
//	if err != nil {
//		return 0, badStringError("bad Content-Length", cl)
//	}
//	return int64(n), nil
//
//}
//
//// Parse the trailer header.
//func fixTrailer(header Header, chunked bool) (Header, error) {
//	vv, ok := header["Trailer"]
//	if !ok {
//		return nil, nil
//	}
//	if !chunked {
//		// Trailer and no chunking:
//		// this is an invalid use case for trailer header.
//		// Nevertheless, no error will be returned and we
//		// let users decide if this is a valid HTTP message.
//		// The Trailer header will be kept in Response.Header
//		// but not populate Response.Trailer.
//		// See issue #27197.
//		return nil, nil
//	}
//	header.Del("Trailer")
//
//	trailer := make(Header)
//	var err error
//	for _, v := range vv {
//		foreachHeaderElement(v, func(key string) {
//			key = CanonicalHeaderKey(key)
//			switch key {
//			case "Transfer-Encoding", "Trailer", "Content-Length":
//				if err == nil {
//					err = badStringError("bad trailer key", key)
//					return
//				}
//			}
//			trailer[key] = nil
//		})
//	}
//	if err != nil {
//		return nil, err
//	}
//	if len(trailer) == 0 {
//		return nil, nil
//	}
//	return trailer, nil
//}
//
//// splitTwoDigitNumber splits a two-digit number into two digits.
func splitTwoDigitNumber(num int) (int, int) {
	tens := num / 10
	ones := num % 10
	return tens, ones
}
//
//// lower returns the ASCII lowercase version of b.
//func lower(b byte) byte {
//	if 'A' <= b && b <= 'Z' {
//		return b + ('a' - 'A')
//	}
//	return b
//}
//
//// foreachHeaderElement splits v according to the "#rule" construction
//// in RFC 7230 section 7 and calls fn for each non-empty element.
//func foreachHeaderElement(v string, fn func(string)) {
//	v = textproto.TrimString(v)
//	if v == "" {
//		return
//	}
//	if !strings.Contains(v, ",") {
//		fn(v)
//		return
//	}
//	for _, f := range strings.Split(v, ",") {
//		if f = textproto.TrimString(f); f != "" {
//			fn(f)
//		}
//	}
//}
//
//func noResponseBodyExpected(requestMethod string) bool {
//	return requestMethod == "HEAD"
//}
//
//func badStringError(what, val string) error { return fmt.Errorf("%s %q", what, val) }
//
//// bodyAllowedForStatus reports whether a given response status code
//// permits a body. See RFC 7230, section 3.3.
//func bodyAllowedForStatus(status int) bool {
//	switch {
//	case status >= 100 && status <= 199:
//		return false
//	case status == 204:
//		return false
//	case status == 304:
//		return false
//	}
//	return true
//}
