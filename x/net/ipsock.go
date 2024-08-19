package net

// SplitHostPort splits a network address of the form "host:port",
// "host%zone:port", "[host]:port" or "[host%zone]:port" into host or
// host%zone and port.
//
// A literal IPv6 address in hostport must be enclosed in square
// brackets, as in "[::1]:80", "[::1%lo0]:80".
//
// See func Dial for a description of the hostport parameter, and host
// and port results.
func SplitHostPort(hostport string) (host, port string, err error) {
	const (
		missingPort   = "missing port in address"
		tooManyColons = "too many colons in address"
	)
	addrErr := func(addr, why string) (host, port string, err error) {
		return "", "", &AddrError{Err: why, Addr: addr}
	}
	j, k := 0, 0

	// The port starts after the last colon.
	i := LastIndexByteString(hostport, ':')
	if i < 0 {
		return addrErr(hostport, missingPort)
	}

	if hostport[0] == '[' {
		// Expect the first ']' just before the last ':'.
		end := IndexByteString(hostport, ']')
		if end < 0 {
			return addrErr(hostport, "missing ']' in address")
		}
		switch end + 1 {
		case len(hostport):
			// There can't be a ':' behind the ']' now.
			return addrErr(hostport, missingPort)
		case i:
			// The expected result.
		default:
			// Either ']' isn't followed by a colon, or it is
			// followed by a colon that is not the last one.
			if hostport[end+1] == ':' {
				return addrErr(hostport, tooManyColons)
			}
			return addrErr(hostport, missingPort)
		}
		host = hostport[1:end]
		j, k = 1, end+1 // there can't be a '[' resp. ']' before these positions
	} else {
		host = hostport[:i]
		if IndexByteString(host, ':') >= 0 {
			return addrErr(hostport, tooManyColons)
		}
	}
	if IndexByteString(hostport[j:], '[') >= 0 {
		return addrErr(hostport, "unexpected '[' in address")
	}
	if IndexByteString(hostport[k:], ']') >= 0 {
		return addrErr(hostport, "unexpected ']' in address")
	}

	port = hostport[i+1:]
	return host, port, nil
}