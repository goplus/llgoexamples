package http

import "strings"

// splitTwoDigitNumber splits a two-digit number into two digits.
func splitTwoDigitNumber(num int) (int, int) {
	tens := num / 10
	ones := num % 10
	return tens, ones
}

func isNotToken(r rune) bool {
	return !IsTokenRune(r)
}

// removeEmptyPort strips the empty port in ":port" to ""
// as mandated by RFC 3986 Section 6.2.3.
func removeEmptyPort(host string) string {
	if hasPort(host) {
		return strings.TrimSuffix(host, ":")
	}
	return host
}

// Given a string of the form "host", "host:port", or "[ipv6::address]:port",
// return true if the string includes a port.
func hasPort(s string) bool { return strings.LastIndex(s, ":") > strings.LastIndex(s, "]") }
