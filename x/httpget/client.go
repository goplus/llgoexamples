package httpget

type Client struct {
	Transport RoundTripper
}

var DefaultClient = &Client{}

type RoundTripper interface {
	RoundTrip(*Request) (*Response, error)
}

func (c *Client) transport() RoundTripper {
	if c.Transport != nil {
		return c.Transport
	}
	return DefaultTransport
}

func Get(url string) (*Response, error) {
	return DefaultClient.Get(url)
}

func (c *Client) Get(url string) (*Response, error) {
	req, err := NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}

func (c *Client) Do(req *Request) (*Response, error) {
	return c.do(req)
}

func (c *Client) do(req *Request) (*Response, error) {
	return c.send(req, nil)
}

func (c *Client) send(req *Request, deadline any) (*Response, error) {
	return send(req, c.transport(), deadline)
}

func send(req *Request, rt RoundTripper, deadline any) (resp *Response, err error) {
	return rt.RoundTrip(req)
}
