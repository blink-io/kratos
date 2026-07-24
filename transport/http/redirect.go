package http

// Redirect is a concrete Redirector implementation. It is exported so that
// alternative transport implementations (e.g. HTTP/3) can construct
// redirects with the same encoding contract.
type Redirect struct {
	URL  string
	Code int
}

func (r *Redirect) Redirect() (string, int) {
	return r.URL, r.Code
}

func (r *Redirect) Error() string {
	return "redirect to " + r.URL
}

// NewRedirect new a redirect with url, which may be a path relative to the request path.
// The provided code should be in the 3xx range and is usually StatusMovedPermanently, StatusFound or StatusSeeOther.
// If the Content-Type header has not been set, Redirect sets it to "text/html; charset=utf-8" and writes a small HTML body.
// Setting the Content-Type header to any value, including nil, disables that behavior.
func NewRedirect(url string, code int) Redirector {
	return &Redirect{URL: url, Code: code}
}
