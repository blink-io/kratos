package http3

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"

	"github.com/go-kratos/kratos/v3/encoding"
	"github.com/go-kratos/kratos/v3/errors"
	"github.com/go-kratos/kratos/v3/internal/host"
	"github.com/go-kratos/kratos/v3/internal/httputil"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/registry"
	"github.com/go-kratos/kratos/v3/selector"
	"github.com/go-kratos/kratos/v3/selector/wrr"
	"github.com/go-kratos/kratos/v3/transport"
)

func init() {
	if selector.GlobalSelector() == nil {
		selector.SetGlobalSelector(wrr.NewBuilder())
	}
}

// DecodeErrorFunc is decode error func.
type DecodeErrorFunc func(ctx context.Context, res *http.Response) error

// EncodeRequestFunc is request encode func.
type EncodeRequestFunc func(ctx context.Context, contentType string, in any) (body []byte, err error)

// DecodeResponseFunc is response decode func.
type DecodeResponseFunc func(ctx context.Context, res *http.Response, out any) error

// ClientOption is HTTP/3 client option.
type ClientOption func(*clientOptions)

type clientOptions struct {
	ctx          context.Context
	tlsConf      *tls.Config
	timeout      time.Duration
	endpoint     string
	userAgent    string
	encoder      EncodeRequestFunc
	decoder      DecodeResponseFunc
	errorDecoder DecodeErrorFunc
	transport    http.RoundTripper
	quicConf     *quic.Config
	nodeFilters  []selector.NodeFilter
	discovery    registry.Discovery
	middleware   []middleware.Middleware
	block        bool
	subsetSize   int
}

// WithSubset with client discovery subset size.
// zero value means subset filter disabled.
func WithSubset(size int) ClientOption {
	return func(o *clientOptions) {
		o.subsetSize = size
	}
}

// WithTransport with client transport.
func WithTransport(trans http.RoundTripper) ClientOption {
	return func(o *clientOptions) {
		o.transport = trans
	}
}

// WithTimeout with client request timeout.
func WithTimeout(d time.Duration) ClientOption {
	return func(o *clientOptions) {
		o.timeout = d
	}
}

// WithUserAgent with client user agent.
func WithUserAgent(ua string) ClientOption {
	return func(o *clientOptions) {
		o.userAgent = ua
	}
}

// WithMiddleware with client middleware.
func WithMiddleware(m ...middleware.Middleware) ClientOption {
	return func(o *clientOptions) {
		o.middleware = m
	}
}

// WithEndpoint with client addr.
func WithEndpoint(endpoint string) ClientOption {
	return func(o *clientOptions) {
		o.endpoint = endpoint
	}
}

// WithRequestEncoder with client request encoder.
func WithRequestEncoder(encoder EncodeRequestFunc) ClientOption {
	return func(o *clientOptions) {
		o.encoder = encoder
	}
}

// WithResponseDecoder with client response decoder.
func WithResponseDecoder(decoder DecodeResponseFunc) ClientOption {
	return func(o *clientOptions) {
		o.decoder = decoder
	}
}

// WithErrorDecoder with client error decoder.
func WithErrorDecoder(errorDecoder DecodeErrorFunc) ClientOption {
	return func(o *clientOptions) {
		o.errorDecoder = errorDecoder
	}
}

// WithDiscovery with client discovery.
func WithDiscovery(d registry.Discovery) ClientOption {
	return func(o *clientOptions) {
		o.discovery = d
	}
}

// WithNodeFilter with select filters.
func WithNodeFilter(filters ...selector.NodeFilter) ClientOption {
	return func(o *clientOptions) {
		o.nodeFilters = filters
	}
}

// WithBlock with client block.
func WithBlock() ClientOption {
	return func(o *clientOptions) {
		o.block = true
	}
}

// WithTLSConfig with tls config. HTTP/3 requires TLS for the QUIC handshake;
// the NextProtos is left alone so the client can advertise other protocols
// when desired.
func WithTLSConfig(c *tls.Config) ClientOption {
	return func(o *clientOptions) {
		o.tlsConf = c
	}
}

// WithQUICConfig with custom QUIC configuration applied to the underlying
// http3.Transport (e.g. to enable datagrams).
func WithQUICConfig(c *quic.Config) ClientOption {
	return func(o *clientOptions) {
		o.quicConf = c
	}
}

// Client is an HTTP/3 client.
type Client struct {
	opts     clientOptions
	target   *Target
	r        *resolver
	cc       *http.Client
	selector selector.Selector
}

// NewClient returns an HTTP/3 client.
func NewClient(ctx context.Context, opts ...ClientOption) (*Client, error) {
	options := clientOptions{
		ctx:          ctx,
		timeout:      2000 * time.Millisecond,
		encoder:      DefaultRequestEncoder,
		decoder:      DefaultResponseDecoder,
		errorDecoder: DefaultErrorDecoder,
		subsetSize:   25,
	}
	for _, o := range opts {
		o(&options)
	}
	if options.transport == nil {
		ht := &http3.Transport{
			TLSClientConfig: options.tlsConf,
			QUICConfig:      options.quicConf,
		}
		options.transport = ht
	}
	target, err := parseTarget(options.endpoint)
	if err != nil {
		return nil, err
	}
	selector := selector.GlobalSelector().Build()
	var r *resolver
	if options.discovery != nil {
		if target.Scheme == schemeDiscovery {
			if r, err = newResolver(ctx, options.discovery, target, selector, options.block, options.subsetSize); err != nil {
				return nil, fmt.Errorf("[http3 client] new resolver failed for endpoint %q: %w", options.endpoint, err)
			}
		} else if _, _, err := host.ExtractHostPort(options.endpoint); err != nil {
			return nil, fmt.Errorf("[http3 client] invalid endpoint format %q: %w", options.endpoint, err)
		}
	}
	return &Client{
		opts:     options,
		target:   target,
		r:        r,
		selector: selector,
		cc: &http.Client{
			Timeout:   options.timeout,
			Transport: options.transport,
		},
	}, nil
}

// Invoke makes an RPC call procedure for remote service.
func (client *Client) Invoke(ctx context.Context, method, path string, args any, reply any, opts ...CallOption) error {
	var (
		contentType string
		body        io.Reader
	)
	c := defaultCallInfo(path)
	for _, o := range opts {
		if err := o.Before(&c); err != nil {
			return err
		}
	}
	if args != nil {
		data, err := client.opts.encoder(ctx, c.ContentType, args)
		if err != nil {
			return err
		}
		contentType = c.ContentType
		body = bytes.NewReader(data)
	}
	url := fmt.Sprintf("%s://%s%s", client.target.Scheme, client.target.Authority, path)
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return err
	}
	if c.HeaderCarrier != nil {
		req.Header = *c.HeaderCarrier
	}

	if contentType != "" {
		req.Header.Set("Content-Type", c.ContentType)
	}
	if c.Accept != "" {
		req.Header.Set("Accept", c.Accept)
	}
	if client.opts.userAgent != "" {
		req.Header.Set("User-Agent", client.opts.userAgent)
	}
	ctx = transport.NewClientContext(ctx, &Transport{
		endpoint:     client.opts.endpoint,
		reqHeader:    headerCarrier(req.Header),
		operation:    c.Operation,
		request:      req,
		pathTemplate: c.PathTemplate,
	})
	return client.invoke(ctx, req, args, reply, c, opts...)
}

func (client *Client) invoke(ctx context.Context, req *http.Request, args any, reply any, c CallInfo, opts ...CallOption) error {
	h := func(ctx context.Context, _ any) (any, error) {
		res, err := client.do(req.WithContext(ctx))
		if res != nil {
			cs := CsAttempt{Res: res}
			for _, o := range opts {
				o.After(&c, &cs)
			}
		}
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if err := client.opts.decoder(ctx, res, reply); err != nil {
			return nil, err
		}
		return reply, nil
	}
	var p selector.Peer
	ctx = selector.NewPeerContext(ctx, &p)
	if len(client.opts.middleware) > 0 {
		h = middleware.Chain(client.opts.middleware...)(h)
	}
	_, err := h(ctx, args)
	return err
}

// Do send an HTTP/3 request and decodes the body of response into target.
// returns an error (of type *Error) if the response status code is not 2xx.
func (client *Client) Do(req *http.Request, opts ...CallOption) (*http.Response, error) {
	c := defaultCallInfo(req.URL.Path)
	for _, o := range opts {
		if err := o.Before(&c); err != nil {
			return nil, err
		}
	}

	return client.do(req)
}

func (client *Client) do(req *http.Request) (*http.Response, error) {
	var done func(context.Context, selector.DoneInfo)
	if client.r != nil {
		var (
			err  error
			node selector.Node
		)
		if node, done, err = client.selector.Select(req.Context(), selector.WithNodeFilter(client.opts.nodeFilters...)); err != nil {
			return nil, errors.ServiceUnavailable("NODE_NOT_FOUND", err.Error())
		}
		req.URL.Scheme = schemeHTTPS
		req.URL.Host = node.Address()
		req.Host = node.Address()
	}
	resp, err := client.cc.Do(req)
	if err == nil {
		t, ok := transport.FromClientContext(req.Context())
		if ok {
			ht, ok := t.(*Transport)
			if ok {
				ht.replyHeader = headerCarrier(resp.Header)
			}
		}
		err = client.opts.errorDecoder(req.Context(), resp)
	}
	if done != nil {
		done(req.Context(), selector.DoneInfo{Err: err})
	}
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Close tears down the Transport and all underlying connections.
func (client *Client) Close() error {
	if client.r != nil {
		_ = client.r.Close()
	}
	if tr, ok := client.opts.transport.(io.Closer); ok {
		return tr.Close()
	}
	return nil
}

// DefaultRequestEncoder is an HTTP/3 request encoder.
func DefaultRequestEncoder(_ context.Context, contentType string, in any) ([]byte, error) {
	if body, ok := httpBody(in); ok {
		return body.GetData(), nil
	}
	name := httputil.ContentSubtype(contentType)
	codec := encoding.GetCodec(name)
	if codec == nil {
		return nil, errors.BadRequest("CODEC", fmt.Sprintf("unregister Content-Type: %s", contentType))
	}
	body, err := codec.Marshal(in)
	if err != nil {
		return nil, err
	}
	return body, err
}

// DefaultResponseDecoder is an HTTP/3 response decoder.
func DefaultResponseDecoder(_ context.Context, res *http.Response, v any) error {
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if body, ok := httpBody(v); ok {
		body.ContentType = res.Header.Get("Content-Type")
		body.Data = data
		return nil
	}
	return CodecForResponse(res).Unmarshal(data, v)
}

// DefaultErrorDecoder is an HTTP/3 error decoder.
func DefaultErrorDecoder(_ context.Context, res *http.Response) error {
	if res.StatusCode >= 200 && res.StatusCode <= 299 {
		return nil
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err == nil {
		e := new(errors.Error)
		if err = CodecForResponse(res).Unmarshal(data, e); err == nil {
			e.Code = int32(res.StatusCode)
			return e
		}
	}
	return errors.Newf(res.StatusCode, errors.UnknownReason, "").WithCause(err)
}

// CodecForResponse get encoding.Codec via http.Response.
func CodecForResponse(r *http.Response) encoding.Codec {
	codec := encoding.GetCodec(httputil.ContentSubtype(r.Header.Get("Content-Type")))
	if codec != nil {
		return codec
	}
	return encoding.GetCodec("json")
}
