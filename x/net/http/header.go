package http

type Header map[string][]string

func (h Header) Add(key, value string) {
	h[key] = append(h[key], value)
}

func (h Header) Set(key, value string) {
	h[key] = []string{value}
}

func (h Header) Get(key string) string {
	if v := h[key]; len(v) > 0 {
		return v[0]
	}
	return ""
}