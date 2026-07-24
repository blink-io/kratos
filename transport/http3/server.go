package http3

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"

	"github.com/go-kratos/kratos/v3/internal/endpoint"
	"github.com/go-kratos/kratos/v3/log"
	"github.com/go-kratos/kratos/v3/middleware"
	khttp "github.com/go-kratos/kratos/v3/transport/http"
)

var (
	_ transportServer     = (*Server)(nil)
	_ transportEndpointer = (*Server)(nil)
	_ http.Handler        = (*Server)(nil)
)

// Aliased interfaces so the linter is happy without importing the upstream
// transport package twice.
type (
	transportServer     interface{ Start(context.Context) error; Stop(context.Context) error }
	transportEndpointer interface{ Endpoint() (*url.URL, error) }
)

// ServerOption is an HTTP/3 server option.
type ServerOption func(*Server)

// Network only allows "udp" for HTTP/3. It is kept for symmetry with the
// transport/http package; non-udp values are ignored.
func Network(network string) ServerOption {
	return func(s *Server) {
		if network == "udp" {
			s.network = network
		}
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

// Middleware with service middleware option. The match-all selector "/" is
// used so the middleware applies to every HTTP/3 route.
func Middleware(m ...middleware.Middleware) ServerOption {
	return func(s *Server) {
		s.middlewares = append(s.middlewares, m...)
	}
}

// Filter with HTTP/3 middleware option.
func Filter(filters ...FilterFunc) ServerOption {
	return func(s *Server) {
		s.filters = append(s.filters, filters...)
	}
}

// RequestVarsDecoder with request decoder.
func RequestVarsDecoder(dec khttp.DecodeRequestFunc) ServerOption {
	return func(s *Server) {
		s.decVars = dec
	}
}

// RequestQueryDecoder with request decoder.
func RequestQueryDecoder(dec khttp.DecodeRequestFunc) ServerOption {
	return func(s *Server) {
		s.decQuery = dec
	}
}

// RequestDecoder with request decoder.
func RequestDecoder(dec khttp.DecodeRequestFunc) ServerOption {
	return func(s *Server) {
		s.decBody = dec
	}
}

// ResponseEncoder with response encoder.
func ResponseEncoder(en khttp.EncodeResponseFunc) ServerOption {
	return func(s *Server) {
		s.enc = en
	}
}

// ErrorEncoder with error encoder.
func ErrorEncoder(en khttp.EncodeErrorFunc) ServerOption {
	return func(s *Server) {
		s.ene = en
	}
}

// TLSConfig with TLS config. HTTP/3 requires TLS for the underlying QUIC
// handshake; the NextProtos field is rewritten to advertise h3 via
// http3.ConfigureTLSConfig unless the caller already configured it.
func TLSConfig(c *tls.Config) ServerOption {
	return func(s *Server) {
		s.tlsConf = c
	}
}

// QUICConfig with custom QUIC configuration applied to the underlying quic-go
// transport (e.g. to enable datagrams or tweak keep-alive).
func QUICConfig(c *quic.Config) ServerOption {
	return func(s *Server) {
		s.quicConf = c
	}
}

// StrictSlash is with router's StrictSlash option.
func StrictSlash(strictSlash bool) ServerOption {
	return func(s *Server) {
		s.strictSlash = &strictSlash
	}
}

// Listener with server UDP listener.
func Listener(lis net.PacketConn) ServerOption {
	return func(s *Server) {
		s.lis = lis
	}
}

// NotFoundHandler with 404 handler.
func NotFoundHandler(handler http.Handler) ServerOption {
	return func(s *Server) {
		s.notFoundHandler = handler
	}
}

// MethodNotAllowedHandler with 405 handler.
func MethodNotAllowedHandler(handler http.Handler) ServerOption {
	return func(s *Server) {
		s.methodNotAllowed = handler
	}
}

// Server is an HTTP/3 server wrapper.
//
// HTTP/3 serves the same wire HTTP semantics as HTTP/1.1 or HTTP/2, so the
// routing, middleware, filter, decoder/encoder and path matching logic is
// reused verbatim from transport/http (constructed as inner). The only
// HTTP/3-specific concerns are the UDP/QUIC transport, the ALPN protocol
// negotiation, and the lifecycle of the quic-go listener.
type Server struct {
	*http3.Server
	inner *khttp.Server

	lis      net.PacketConn
	tlsConf  *tls.Config
	quicConf *quic.Config

	network  string
	address  string
	timeout  time.Duration
	endpoint *url.URL
	err      error

	strictSlash     *bool
	middlewares     []middleware.Middleware
	filters         []FilterFunc
	decVars         khttp.DecodeRequestFunc
	decQuery        khttp.DecodeRequestFunc
	decBody         khttp.DecodeRequestFunc
	enc             khttp.EncodeResponseFunc
	ene             khttp.EncodeErrorFunc
	notFoundHandler http.Handler
	methodNotAllowed http.Handler
}

// NewServer creates an HTTP/3 server by options.
func NewServer(opts ...ServerOption) *Server {
	srv := &Server{
		network:      "udp",
		address:      ":0",
		timeout:      1 * time.Second,
		middlewares:  make([]middleware.Middleware, 0),
		filters:      make([]FilterFunc, 0),
	}
	for _, o := range opts {
		o(srv)
	}
	// Build the inner transport/http.Server with the forwarded options so
	// routing, middleware, decoder/encoder and strict-slash wiring match the
	// HTTP/1.1 server out of the box.
	innerOpts := []khttp.ServerOption{
		khttp.Timeout(srv.timeout),
		khttp.Middleware(srv.middlewares...),
		khttp.Filter(srv.filters...),
	}
	if srv.decVars != nil {
		innerOpts = append(innerOpts, khttp.RequestVarsDecoder(srv.decVars))
	}
	if srv.decQuery != nil {
		innerOpts = append(innerOpts, khttp.RequestQueryDecoder(srv.decQuery))
	}
	if srv.decBody != nil {
		innerOpts = append(innerOpts, khttp.RequestDecoder(srv.decBody))
	}
	if srv.enc != nil {
		innerOpts = append(innerOpts, khttp.ResponseEncoder(srv.enc))
	}
	if srv.ene != nil {
		innerOpts = append(innerOpts, khttp.ErrorEncoder(srv.ene))
	}
	if srv.strictSlash != nil {
		innerOpts = append(innerOpts, khttp.StrictSlash(*srv.strictSlash))
	}
	if srv.notFoundHandler != nil {
		innerOpts = append(innerOpts, khttp.NotFoundHandler(srv.notFoundHandler))
	}
	if srv.methodNotAllowed != nil {
		innerOpts = append(innerOpts, khttp.MethodNotAllowedHandler(srv.methodNotAllowed))
	}
	srv.Server = &http3.Server{}
	srv.inner = khttp.NewServer(innerOpts...)
	srv.Server.Handler = http.HandlerFunc(srv.inner.ServeHTTP)
	return srv
}

// Route registers an HTTP/3 router.
func (s *Server) Route(prefix string, filters ...FilterFunc) *Router {
	return &Router{inner: s.inner.Route(prefix, filters...)}
}

// Handle registers a new route with a matcher for the URL path.
func (s *Server) Handle(path string, h http.Handler) {
	s.inner.Handle(path, h)
}

// HandlePrefix registers a new route with a matcher for the URL path prefix.
func (s *Server) HandlePrefix(prefix string, h http.Handler) {
	s.inner.HandlePrefix(prefix, h)
}

// HandleFunc registers a new route with a matcher for the URL path.
func (s *Server) HandleFunc(path string, h http.HandlerFunc) {
	s.inner.HandleFunc(path, h)
}

// HandleHeader registers a new route with a matcher for the header.
func (s *Server) HandleHeader(key, val string, h http.HandlerFunc) {
	s.inner.HandleHeader(key, val, h)
}

// Use uses a service middleware with selector.
func (s *Server) Use(selector string, m ...middleware.Middleware) {
	s.inner.Use(selector, m...)
}

// WalkRoute walks the router and all its sub-routers, calling walkFn for each
// route in the tree.
func (s *Server) WalkRoute(fn WalkRouteFunc) error {
	return s.inner.WalkRoute(fn)
}

// WalkHandle walks the router and all its sub-routers, calling walkFn for each
// route in the tree.
func (s *Server) WalkHandle(handle func(method, path string, handler http.HandlerFunc)) error {
	return s.inner.WalkHandle(handle)
}

// ServeHTTP exposes the HTTP/3 handler so it can be used as an http.Handler.
func (s *Server) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	s.Server.Handler.ServeHTTP(res, req)
}

// Endpoint return a real address to registry endpoint.
// examples:
//
//	https://127.0.0.1:443
func (s *Server) Endpoint() (*url.URL, error) {
	if err := s.listenAndEndpoint(); err != nil {
		return nil, err
	}
	return s.endpoint, nil
}

// Start start the HTTP/3 server.
func (s *Server) Start(ctx context.Context) error {
	if err := s.listenAndEndpoint(); err != nil {
		return err
	}
	log.Info("[HTTP3] server listening", "addr", s.lis.LocalAddr().String())
	if err := s.Server.Serve(s.lis); err != nil {
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
	return nil
}

// Stop stop the HTTP/3 server.
func (s *Server) Stop(ctx context.Context) error {
	log.Info("[HTTP3] server stopping")
	err := s.Server.Shutdown(ctx)
	if err != nil {
		if ctx.Err() != nil {
			log.Warn("[HTTP3] server couldn't stop gracefully in time, doing force stop")
			err = s.Server.Close()
		}
	}
	return err
}

func (s *Server) listenAndEndpoint() error {
	if s.lis == nil {
		addr := s.address
		if addr == "" {
			addr = ":https"
		}
		udpAddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			s.err = err
			return err
		}
		conn, err := net.ListenUDP("udp", udpAddr)
		if err != nil {
			s.err = err
			return err
		}
		s.lis = conn
	}
	if s.tlsConf == nil {
		s.err = errors.New("http3: TLSConfig is required")
		return s.err
	}
	s.Server.TLSConfig = http3.ConfigureTLSConfig(s.tlsConf)
	if s.quicConf != nil {
		s.Server.QUICConfig = s.quicConf
	}
	if s.endpoint == nil {
		addr, err := extractUDPAddr(s.address, s.lis)
		if err != nil {
			s.err = err
			return err
		}
		s.endpoint = endpoint.NewEndpoint("https", addr)
	}
	return s.err
}

// extractUDPAddr mirrors host.Extract but for a UDP packet conn. It resolves
// the bound port and rewrites wildcard hosts to a routable interface IP.
func extractUDPAddr(hostPort string, lis net.PacketConn) (string, error) {
	addr, port, err := net.SplitHostPort(hostPort)
	if err != nil && lis == nil {
		return "", err
	}
	if lis != nil {
		if la, ok := lis.LocalAddr().(*net.UDPAddr); ok {
			port = strconv.Itoa(la.Port)
		} else {
			return "", fmt.Errorf("http3: failed to extract port from %v", lis.LocalAddr())
		}
	}
	if len(addr) > 0 && (addr != "0.0.0.0" && addr != "[::]" && addr != "::") {
		return net.JoinHostPort(addr, port), nil
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	var (
		minIndex = 0
		ips      = make([]net.IP, 0, 1)
	)
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Index >= minIndex && len(ips) != 0 {
			continue
		}
		ifaceAddrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, rawAddr := range ifaceAddrs {
			var ip net.IP
			switch a := rawAddr.(type) {
			case *net.IPAddr:
				ip = a.IP
			case *net.IPNet:
				ip = a.IP
			default:
				continue
			}
			if ip.IsGlobalUnicast() && !ip.IsInterfaceLocalMulticast() {
				minIndex = iface.Index
				ips = append(ips, ip)
				if ip.To4() != nil {
					break
				}
			}
		}
	}
	if len(ips) != 0 {
		return net.JoinHostPort(ips[len(ips)-1].String(), port), nil
	}
	return "", nil
}
