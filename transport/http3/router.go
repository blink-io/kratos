package http3

import (
	khttp "github.com/go-kratos/kratos/v3/transport/http"
)

// WalkRouteFunc is the type of the function called for each route visited by
// WalkRoute.
type WalkRouteFunc = khttp.WalkRouteFunc

// RouteInfo is an HTTP/3 route info.
type RouteInfo = khttp.RouteInfo

// HandlerFunc defines a function to serve HTTP/3 requests.
type HandlerFunc = khttp.HandlerFunc

// Router is an HTTP/3 router.
//
// HTTP/3 routing is identical to HTTP/1.1 because both serve the same
// net/http request/response model. The Router is a thin wrapper that defers
// all routing decisions to the transport/http router built on top of chi.
type Router struct {
	inner *khttp.Router
}

// Group returns a new router group.
func (r *Router) Group(prefix string, filters ...FilterFunc) *Router {
	return &Router{inner: r.inner.Group(prefix, filters...)}
}

// Handle registers a new route with a matcher for the URL path and method.
func (r *Router) Handle(method, relativePath string, h HandlerFunc, filters ...FilterFunc) {
	r.inner.Handle(method, relativePath, h, filters...)
}

// GET registers a new GET route for a path with matching handler in the router.
func (r *Router) GET(path string, h HandlerFunc, m ...FilterFunc) {
	r.inner.GET(path, h, m...)
}

// HEAD registers a new HEAD route for a path with matching handler in the router.
func (r *Router) HEAD(path string, h HandlerFunc, m ...FilterFunc) {
	r.inner.HEAD(path, h, m...)
}

// POST registers a new POST route for a path with matching handler in the router.
func (r *Router) POST(path string, h HandlerFunc, m ...FilterFunc) {
	r.inner.POST(path, h, m...)
}

// PUT registers a new PUT route for a path with matching handler in the router.
func (r *Router) PUT(path string, h HandlerFunc, m ...FilterFunc) {
	r.inner.PUT(path, h, m...)
}

// PATCH registers a new PATCH route for a path with matching handler in the router.
func (r *Router) PATCH(path string, h HandlerFunc, m ...FilterFunc) {
	r.inner.PATCH(path, h, m...)
}

// DELETE registers a new DELETE route for a path with matching handler in the router.
func (r *Router) DELETE(path string, h HandlerFunc, m ...FilterFunc) {
	r.inner.DELETE(path, h, m...)
}

// CONNECT registers a new CONNECT route for a path with matching handler in the router.
func (r *Router) CONNECT(path string, h HandlerFunc, m ...FilterFunc) {
	r.inner.CONNECT(path, h, m...)
}

// OPTIONS registers a new OPTIONS route for a path with matching handler in the router.
func (r *Router) OPTIONS(path string, h HandlerFunc, m ...FilterFunc) {
	r.inner.OPTIONS(path, h, m...)
}

// TRACE registers a new TRACE route for a path with matching handler in the router.
func (r *Router) TRACE(path string, h HandlerFunc, m ...FilterFunc) {
	r.inner.TRACE(path, h, m...)
}
