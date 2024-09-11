package http

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"

	"github.com/goplus/llgoexamples/x/net"
)

/**
 * Copied from the libraries that llgo cannot be used
 */

var isTokenTable = [127]bool{ // httpguts.isTokenTable
	'!':  true,
	'#':  true,
	'$':  true,
	'%':  true,
	'&':  true,
	'\'': true,
	'*':  true,
	'+':  true,
	'-':  true,
	'.':  true,
	'0':  true,
	'1':  true,
	'2':  true,
	'3':  true,
	'4':  true,
	'5':  true,
	'6':  true,
	'7':  true,
	'8':  true,
	'9':  true,
	'A':  true,
	'B':  true,
	'C':  true,
	'D':  true,
	'E':  true,
	'F':  true,
	'G':  true,
	'H':  true,
	'I':  true,
	'J':  true,
	'K':  true,
	'L':  true,
	'M':  true,
	'N':  true,
	'O':  true,
	'P':  true,
	'Q':  true,
	'R':  true,
	'S':  true,
	'T':  true,
	'U':  true,
	'W':  true,
	'V':  true,
	'X':  true,
	'Y':  true,
	'Z':  true,
	'^':  true,
	'_':  true,
	'`':  true,
	'a':  true,
	'b':  true,
	'c':  true,
	'd':  true,
	'e':  true,
	'f':  true,
	'g':  true,
	'h':  true,
	'i':  true,
	'j':  true,
	'k':  true,
	'l':  true,
	'm':  true,
	'n':  true,
	'o':  true,
	'p':  true,
	'q':  true,
	'r':  true,
	's':  true,
	't':  true,
	'u':  true,
	'v':  true,
	'w':  true,
	'x':  true,
	'y':  true,
	'z':  true,
	'|':  true,
	'~':  true,
}

func IsTokenRune(r rune) bool { // httpguts.IsTokenRune
	i := int(r)
	return i < len(isTokenTable) && isTokenTable[i]
}

// ValidHeaderFieldName reports whether v is a valid HTTP/1.x header name.
// HTTP/2 imposes the additional restriction that uppercase ASCII
// letters are not allowed.
//
// RFC 7230 says:
//
//	header-field   = field-name ":" OWS field-value OWS
//	field-name     = token
//	token          = 1*tchar
//	tchar = "!" / "#" / "$" / "%" / "&" / "'" / "*" / "+" / "-" / "." /
//	        "^" / "_" / "`" / "|" / "~" / DIGIT / ALPHA
func ValidHeaderFieldName(v string) bool { // httpguts.ValidHeaderFieldName
	if len(v) == 0 {
		return false
	}
	for i := 0; i < len(v); i++ {
		if !isTokenTable[v[i]] {
			return false
		}
	}
	return true
}

// ValidHeaderFieldValue reports whether v is a valid "field-value" according to
// http://www.w3.org/Protocols/rfc2616/rfc2616-sec4.html#sec4.2 :
//
//	message-header = field-name ":" [ field-value ]
//	field-value    = *( field-content | LWS )
//	field-content  = <the OCTETs making up the field-value
//	                 and consisting of either *TEXT or combinations
//	                 of token, separators, and quoted-string>
//
// http://www.w3.org/Protocols/rfc2616/rfc2616-sec2.html#sec2.2 :
//
//	TEXT           = <any OCTET except CTLs,
//	                  but including LWS>
//	LWS            = [CRLF] 1*( SP | HT )
//	CTL            = <any US-ASCII control character
//	                 (octets 0 - 31) and DEL (127)>
//
// RFC 7230 says:
//
//	field-value    = *( field-content / obs-fold )
//	obj-fold       =  N/A to http2, and deprecated
//	field-content  = field-vchar [ 1*( SP / HTAB ) field-vchar ]
//	field-vchar    = VCHAR / obs-text
//	obs-text       = %x80-FF
//	VCHAR          = "any visible [USASCII] character"
//
// http2 further says: "Similarly, HTTP/2 allows header field values
// that are not valid. While most of the values that can be encoded
// will not alter header field parsing, carriage return (CR, ASCII
// 0xd), line feed (LF, ASCII 0xa), and the zero character (NUL, ASCII
// 0x0) might be exploited by an attacker if they are translated
// verbatim. Any request or response that contains a character not
// permitted in a header field value MUST be treated as malformed
// (Section 8.1.2.6). Valid characters are defined by the
// field-content ABNF rule in Section 3.2 of [RFC7230]."
//
// This function does not (yet?) properly handle the rejection of
// strings that begin or end with SP or HTAB.
func ValidHeaderFieldValue(v string) bool { // httpguts.ValidHeaderFieldValue
	for i := 0; i < len(v); i++ {
		b := v[i]
		if isCTL(b) && !isLWS(b) {
			return false
		}
	}
	return true
}

// isLWS reports whether b is linear white space, according
// to http://www.w3.org/Protocols/rfc2616/rfc2616-sec2.html#sec2.2
//
//	LWS            = [CRLF] 1*( SP | HT )
func isLWS(b byte) bool { return b == ' ' || b == '\t' } // httpguts.isLWS

// isCTL reports whether b is a control byte, according
// to http://www.w3.org/Protocols/rfc2616/rfc2616-sec2.html#sec2.2
//
//	CTL            = <any US-ASCII control character
//	                 (octets 0 - 31) and DEL (127)>
func isCTL(b byte) bool { // httpguts.isCTL
	const del = 0x7f // a CTL
	return b < ' ' || b == del
}

// HeaderValuesContainsToken reports whether any string in values
// contains the provided token, ASCII case-insensitively.
func HeaderValuesContainsToken(values []string, token string) bool { // httpguts.HeaderValuesContainsToken
	for _, v := range values {
		if headerValueContainsToken(v, token) {
			return true
		}
	}
	return false
}

// headerValueContainsToken reports whether v (assumed to be a
// 0#element, in the ABNF extension described in RFC 7230 section 7)
// contains token amongst its comma-separated tokens, ASCII
// case-insensitively.
func headerValueContainsToken(v string, token string) bool { // httpguts.headerValueContainsToken
	for comma := strings.IndexByte(v, ','); comma != -1; comma = strings.IndexByte(v, ',') {
		if tokenEqual(trimOWS(v[:comma]), token) {
			return true
		}
		v = v[comma+1:]
	}
	return tokenEqual(trimOWS(v), token)
}

// PunycodeHostPort returns the IDNA Punycode version
// of the provided "host" or "host:port" string.
func PunycodeHostPort(v string) (string, error) { // httpguts.PunycodeHostPort
	if isASCII(v) {
		return v, nil
	}

	host, port, err := net.SplitHostPort(v)
	if err != nil {
		// The input 'v' argument was just a "host" argument,
		// without a port. This error should not be returned
		// to the caller.
		host = v
		port = ""
	}
	host, err = idna.ToASCII(host)
	if err != nil {
		// Non-UTF-8? Not representable in Punycode, in any
		// case.
		return "", err
	}
	if port == "" {
		return host, nil
	}
	return net.JoinHostPort(host, port), nil
}

func isASCII(s string) bool { // httpguts.isASCII
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// ValidHostHeader reports whether h is a valid host header.
func ValidHostHeader(h string) bool { // httpguts.ValidHostHeader
	// The latest spec is actually this:
	//
	// http://tools.ietf.org/html/rfc7230#section-5.4
	//     Host = uri-host [ ":" port ]
	//
	// Where uri-host is:
	//     http://tools.ietf.org/html/rfc3986#section-3.2.2
	//
	// But we're going to be much more lenient for now and just
	// search for any byte that's not a valid byte in any of those
	// expressions.
	for i := 0; i < len(h); i++ {
		if !validHostByte[h[i]] {
			return false
		}
	}
	return true
}

// See the validHostHeader comment.
var validHostByte = [256]bool{ // httpguts.validHostByte
	'0': true, '1': true, '2': true, '3': true, '4': true, '5': true, '6': true, '7': true,
	'8': true, '9': true,

	'a': true, 'b': true, 'c': true, 'd': true, 'e': true, 'f': true, 'g': true, 'h': true,
	'i': true, 'j': true, 'k': true, 'l': true, 'm': true, 'n': true, 'o': true, 'p': true,
	'q': true, 'r': true, 's': true, 't': true, 'u': true, 'v': true, 'w': true, 'x': true,
	'y': true, 'z': true,

	'A': true, 'B': true, 'C': true, 'D': true, 'E': true, 'F': true, 'G': true, 'H': true,
	'I': true, 'J': true, 'K': true, 'L': true, 'M': true, 'N': true, 'O': true, 'P': true,
	'Q': true, 'R': true, 'S': true, 'T': true, 'U': true, 'V': true, 'W': true, 'X': true,
	'Y': true, 'Z': true,

	'!':  true, // sub-delims
	'$':  true, // sub-delims
	'%':  true, // pct-encoded (and used in IPv6 zones)
	'&':  true, // sub-delims
	'(':  true, // sub-delims
	')':  true, // sub-delims
	'*':  true, // sub-delims
	'+':  true, // sub-delims
	',':  true, // sub-delims
	'-':  true, // unreserved
	'.':  true, // unreserved
	':':  true, // IPv6address + Host expression's optional port
	';':  true, // sub-delims
	'=':  true, // sub-delims
	'[':  true,
	'\'': true, // sub-delims
	']':  true,
	'_':  true, // unreserved
	'~':  true, // unreserved
}

// IsPrint returns whether s is ASCII and printable according to
// https://tools.ietf.org/html/rfc20#section-4.2.
func IsPrint(s string) bool { // ascii.IsPrint
	for i := 0; i < len(s); i++ {
		if s[i] < ' ' || s[i] > '~' {
			return false
		}
	}
	return true
}

// ToLower returns the lowercase version of s if s is ASCII and printable.
func ToLower(s string) (lower string, ok bool) { // ascii.ToLower
	if !IsPrint(s) {
		return "", false
	}
	return strings.ToLower(s), true
}

// EqualFold is strings.EqualFold, ASCII only. It reports whether s and t
// are equal, ASCII-case-insensitively.
func EqualFold(s, t string) bool { // ascii.EqualFold
	if len(s) != len(t) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if lower(s[i]) != lower(t[i]) {
			return false
		}
	}
	return true
}

// lower returns the ASCII lowercase version of b.
func lower(b byte) byte { // ascii.lower
	if 'A' <= b && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

// Is returns whether s is ASCII.
func Is(s string) bool { // ascii.Is
	for i := 0; i < len(s); i++ {
		if s[i] > unicode.MaxASCII {
			return false
		}
	}
	return true
}