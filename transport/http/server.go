package http

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/go-kratos/kratos/v3/internal/endpoint"
	"github.com/go-kratos/kratos/v3/internal/host"
	"github.com/go-kratos/kratos/v3/internal/matcher"
	"github.com/go-kratos/kratos/v3/log"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/transport"
)

var (
	_ transport.Server     = (*Server)(nil)
	_ transport.Endpointer = (*Server)(nil)
	_ http.Handler         = (*Server)(nil)
)

// ServerOption is an HTTP server option.
type ServerOption func(*Server)

// Network with server network.
func Network(network string) ServerOption {
	return func(s *Server) {
		s.network = network
	}
}

// Address with server address.
func Address(addr string) ServerOption {
	return func(s *Server) {
		s.address = addr
	}
}

// Endpoint with server address.
func Endpoint(endpoint *url.URL) ServerOption {
	return func(s *Server) {
		s.endpoint = endpoint
	}
}

// Timeout with server timeout.
func Timeout(timeout time.Duration) ServerOption {
	return func(s *Server) {
		s.timeout = timeout
	}
}

// Middleware with service middleware option.
func Middleware(m ...middleware.Middleware) ServerOption {
	return func(o *Server) {
		o.middleware.Use(m...)
	}
}

// Filter with HTTP middleware option.
func Filter(filters ...FilterFunc) ServerOption {
	return func(o *Server) {
		o.filters = filters
	}
}

// RequestVarsDecoder with request decoder.
func RequestVarsDecoder(dec DecodeRequestFunc) ServerOption {
	return func(o *Server) {
		o.decVars = dec
	}
}

// RequestQueryDecoder with request decoder.
func RequestQueryDecoder(dec DecodeRequestFunc) ServerOption {
	return func(o *Server) {
		o.decQuery = dec
	}
}

// RequestDecoder with request decoder.
func RequestDecoder(dec DecodeRequestFunc) ServerOption {
	return func(o *Server) {
		o.decBody = dec
	}
}

// ResponseEncoder with response encoder.
func ResponseEncoder(en EncodeResponseFunc) ServerOption {
	return func(o *Server) {
		o.enc = en
	}
}

// ErrorEncoder with error encoder.
func ErrorEncoder(en EncodeErrorFunc) ServerOption {
	return func(o *Server) {
		o.ene = en
	}
}

// TLSConfig with TLS config.
func TLSConfig(c *tls.Config) ServerOption {
	return func(o *Server) {
		o.tlsConf = c
	}
}

// StrictSlash is with router's StrictSlash option.
// If true, when the path pattern is "/path/", accessing "/path" will
// redirect to the former and vice versa.
func StrictSlash(strictSlash bool) ServerOption {
	return func(o *Server) {
		o.strictSlash = strictSlash
	}
}

// Listener with server lis
func Listener(lis net.Listener) ServerOption {
	return func(s *Server) {
		s.lis = lis
	}
}

// PathPrefix with router's PathPrefix, router will be replaced by a subrouter that start with prefix.
func PathPrefix(prefix string) ServerOption {
	return func(s *Server) {
		sub := chi.NewRouter()
		s.router.Mount(prefix, sub)
		s.router = sub
	}
}

func NotFoundHandler(handler http.Handler) ServerOption {
	return func(s *Server) {
		s.notFoundHandler = handler
	}
}

func MethodNotAllowedHandler(handler http.Handler) ServerOption {
	return func(s *Server) {
		s.methodNotAllowedHandler = handler
	}
}

// Server is an HTTP server wrapper.
type Server struct {
	*http.Server
	lis                     net.Listener
	tlsConf                 *tls.Config
	endpoint                *url.URL
	err                     error
	network                 string
	address                 string
	timeout                 time.Duration
	filters                 []FilterFunc
	middleware              matcher.Matcher
	decVars                 DecodeRequestFunc
	decQuery                DecodeRequestFunc
	decBody                 DecodeRequestFunc
	enc                     EncodeResponseFunc
	ene                     EncodeErrorFunc
	strictSlash             bool
	router                  *chi.Mux
	prefixMu                sync.RWMutex
	prefixHandlers          []prefixHandlerEntry
	notFoundHandler         http.Handler
	methodNotAllowedHandler http.Handler
}

// prefixHandlerEntry stores a HandlePrefix registration. Entries are
// consulted from the chi NotFound dispatcher so that prefix matching
// replicates gorilla/mux's string-prefix semantics (e.g. prefix "/foo"
// also matches "/foobar") and never overrides a more specific route
// registered via Handle/HandleFunc/Route.
type prefixHandlerEntry struct {
	prefix  string
	handler http.Handler
}

// NewServer creates an HTTP server by options.
func NewServer(opts ...ServerOption) *Server {
	srv := &Server{
		network:                 "tcp",
		address:                 ":0",
		timeout:                 1 * time.Second,
		middleware:              matcher.New(),
		decVars:                 DefaultRequestVars,
		decQuery:                DefaultRequestQuery,
		decBody:                 DefaultRequestDecoder,
		enc:                     DefaultResponseEncoder,
		ene:                     DefaultErrorEncoder,
		strictSlash:             true,
		router:                  chi.NewRouter(),
		prefixHandlers:          make([]prefixHandlerEntry, 0),
		notFoundHandler:         http.DefaultServeMux,
		methodNotAllowedHandler: http.DefaultServeMux,
	}
	srv.router.NotFound(http.HandlerFunc(srv.dispatchNotFound))
	srv.router.MethodNotAllowed(http.HandlerFunc(srv.dispatchMethodNotAllowed))
	for _, o := range opts {
		o(srv)
	}
	srv.router.Use(srv.filter())
	srv.applyStrictSlash()
	srv.Server = &http.Server{
		Handler:   FilterChain(srv.filters...)(srv.router),
		TLSConfig: srv.tlsConf,
	}
	return srv
}

// dispatchNotFound is the chi NotFound handler. It first consults the
// HandlePrefix registrations (longest prefix wins) and only falls back to
// the configured not-found handler when no prefix matches.
func (s *Server) dispatchNotFound(w http.ResponseWriter, r *http.Request) {
	if h := s.matchPrefix(r.URL.Path); h != nil {
		h.ServeHTTP(w, r)
		return
	}
	s.notFoundHandler.ServeHTTP(w, r)
}

// dispatchMethodNotAllowed is the chi MethodNotAllowed handler.
func (s *Server) dispatchMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	s.methodNotAllowedHandler.ServeHTTP(w, r)
}

// matchPrefix returns the handler registered for the longest registered
// prefix that is a leading substring of path, or nil if none matches.
func (s *Server) matchPrefix(path string) http.Handler {
	s.prefixMu.RLock()
	defer s.prefixMu.RUnlock()
	var matched http.Handler
	var longest int
	for _, e := range s.prefixHandlers {
		if len(e.prefix) <= longest {
			continue
		}
		if strings.HasPrefix(path, e.prefix) {
			matched = e.handler
			longest = len(e.prefix)
		}
	}
	return matched
}

// Use uses a service middleware with selector.
// selector:
//   - '/*'
//   - '/helloworld.v1.Greeter/*'
//   - '/helloworld.v1.Greeter/SayHello'
func (s *Server) Use(selector string, m ...middleware.Middleware) {
	s.middleware.Add(selector, m...)
}

// WalkRoute walks the router and all its sub-routers, calling walkFn for each route in the tree.
func (s *Server) WalkRoute(fn WalkRouteFunc) error {
	return chi.Walk(s.router, func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == "*" {
			return nil
		}
		return fn(RouteInfo{Method: method, Path: route})
	})
}

// WalkHandle walks the router and all its sub-routers, calling walkFn for each route in the tree.
func (s *Server) WalkHandle(handle func(method, path string, handler http.HandlerFunc)) error {
	return s.WalkRoute(func(r RouteInfo) error {
		handle(r.Method, r.Path, s.ServeHTTP)
		return nil
	})
}

// Route registers an HTTP router.
func (s *Server) Route(prefix string, filters ...FilterFunc) *Router {
	return newRouter(prefix, s, filters...)
}

// Handle registers a new route with a matcher for the URL path.
func (s *Server) Handle(path string, h http.Handler) {
	s.router.Handle(path, h)
}

// HandlePrefix registers a new route with a matcher for the URL path prefix.
// Behaves like gorilla/mux's PathPrefix(prefix).Handler(h): the handler is
// invoked for every request whose path begins with prefix (including
// overlapping paths such as prefix == "/test/prefix" matching
// "/test/prefixfoo"). Routes registered via Handle/HandleFunc/Route take
// precedence over prefix handlers.
func (s *Server) HandlePrefix(prefix string, h http.Handler) {
	s.prefixMu.Lock()
	s.prefixHandlers = append(s.prefixHandlers, prefixHandlerEntry{prefix: prefix, handler: h})
	s.prefixMu.Unlock()
}

// HandleFunc registers a new route with a matcher for the URL path.
func (s *Server) HandleFunc(path string, h http.HandlerFunc) {
	s.router.HandleFunc(path, h)
}

// HandleHeader registers a new route with a matcher for the header.
func (s *Server) HandleHeader(key, val string, h http.HandlerFunc) {
	s.router.With(headerMatcher(key, val)).HandleFunc("/", h)
}

// ServeHTTP should write reply headers and data to the ResponseWriter and then return.
func (s *Server) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	s.Handler.ServeHTTP(res, req)
}

func (s *Server) filter() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			var (
				ctx    context.Context
				cancel context.CancelFunc
			)
			if s.timeout > 0 {
				ctx, cancel = context.WithTimeout(req.Context(), s.timeout)
			} else {
				ctx, cancel = context.WithCancel(req.Context())
			}
			defer cancel()

			pathTemplate := req.URL.Path
			if rctx := chi.RouteContext(req.Context()); rctx != nil {
				if p := rctx.RoutePattern(); p != "" {
					pathTemplate = p
				}
			}

			tr := &Transport{
				operation:    pathTemplate,
				pathTemplate: pathTemplate,
				reqHeader:    headerCarrier(req.Header),
				replyHeader:  headerCarrier(w.Header()),
				request:      req,
				response:     w,
			}
			if s.endpoint != nil {
				tr.endpoint = s.endpoint.String()
			}
			tr.request = req.WithContext(transport.NewServerContext(ctx, tr))
			next.ServeHTTP(w, tr.request)
		})
	}
}

// applyStrictSlash installs a middleware on the router that replicates the
// gorilla/mux StrictSlash behavior: if a registered route exists only with a
// trailing slash (or without one), requests to the alternate form are
// redirected (301) to the registered form.
func (s *Server) applyStrictSlash() {
	if !s.strictSlash {
		return
	}
	router := s.router
	s.router.Use(strictSlashMiddleware(router))
}

func strictSlashMiddleware(router *chi.Mux) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if path == "" {
				next.ServeHTTP(w, r)
				return
			}
			rctx := chi.NewRouteContext()
			if router.Find(rctx, r.Method, path) != "" {
				next.ServeHTTP(w, r)
				return
			}
			var alt string
			if strings.HasSuffix(path, "/") {
				alt = strings.TrimRight(path, "/")
				if alt == "" {
					next.ServeHTTP(w, r)
					return
				}
			} else {
				alt = path + "/"
			}
			if router.Find(rctx, r.Method, alt) != "" {
				if r.URL.RawQuery != "" {
					alt = alt + "?" + r.URL.RawQuery
				}
				http.Redirect(w, r, alt, http.StatusMovedPermanently)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// headerMatcher returns a middleware that only continues if the request has
// the given header set to the given value. Mismatched/missing headers yield
// a 404, matching gorilla/mux's Headers().Handler() behaviour.
func headerMatcher(key, val string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get(key) != val {
				http.NotFound(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func toHandlerFunc(h http.Handler) http.HandlerFunc {
	if h == nil {
		return http.HandlerFunc(http.NotFound)
	}
	if hf, ok := h.(http.HandlerFunc); ok {
		return hf
	}
	return http.HandlerFunc(h.ServeHTTP)
}

// Endpoint return a real address to registry endpoint.
// examples:
//
//	https://127.0.0.1:8000
//	Legacy: http://127.0.0.1:8000?isSecure=false
func (s *Server) Endpoint() (*url.URL, error) {
	if err := s.listenAndEndpoint(); err != nil {
		return nil, err
	}
	return s.endpoint, nil
}

// Start start the HTTP server.
func (s *Server) Start(ctx context.Context) error {
	if err := s.listenAndEndpoint(); err != nil {
		return err
	}
	s.BaseContext = func(net.Listener) context.Context {
		return ctx
	}
	log.Info("[HTTP] server listening", "addr", s.lis.Addr().String())
	var err error
	if s.tlsConf != nil {
		err = s.ServeTLS(s.lis, "", "")
	} else {
		err = s.Serve(s.lis)
	}
	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Stop stop the HTTP server.
func (s *Server) Stop(ctx context.Context) error {
	log.Info("[HTTP] server stopping")
	err := s.Shutdown(ctx)
	if err != nil {
		if ctx.Err() != nil {
			log.Warn("[HTTP] server couldn't stop gracefully in time, doing force stop")
			err = s.Close()
		}
	}
	return err
}

func (s *Server) listenAndEndpoint() error {
	if s.lis == nil {
		lis, err := net.Listen(s.network, s.address)
		if err != nil {
			s.err = err
			return err
		}
		s.lis = lis
	}
	if s.endpoint == nil {
		addr, err := host.Extract(s.address, s.lis)
		if err != nil {
			s.err = err
			return err
		}
		s.endpoint = endpoint.NewEndpoint(endpoint.Scheme("http", s.tlsConf != nil), addr)
	}
	return s.err
}
