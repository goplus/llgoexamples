package http

// cloneOrMakeHeader invokes Header.Clone but if the
// result is nil, it'll instead make and return a non-nil Header.
func cloneOrMakeHeader(hdr Header) Header {
	clone := hdr.Clone()
	if clone == nil {
		clone = make(Header)
	}
	return clone
}
