package http3

import (
	khttp "github.com/go-kratos/kratos/v3/transport/http"
)

// FilterFunc is a function which receives a http.Handler and returns another http.Handler.
type FilterFunc = khttp.FilterFunc

// FilterChain returns a FilterFunc that specifies the chained handler for HTTP/3 Router.
func FilterChain(filters ...FilterFunc) FilterFunc {
	return khttp.FilterChain(filters...)
}
