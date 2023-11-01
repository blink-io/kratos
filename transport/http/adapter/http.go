package adapter

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
)

type HttpAdapter struct {
	srv *http.Server
}

var _ ServerAdapter = (*HttpAdapter)(nil)

func NewHttpAdapter(ctx context.Context, h http.Handler, tlsConf *tls.Config) ServerAdapter {
	srv := &http.Server{
		BaseContext: func(ln net.Listener) context.Context {
			return ctx
		},
		Handler:   h,
		TLSConfig: tlsConf,
	}
	adp := &HttpAdapter{
		srv: srv,
	}
	return adp
}

func (h *HttpAdapter) Handler() http.Handler {
	//TODO implement me
	panic("implement me")
}

func (h *HttpAdapter) Shutdown(ctx context.Context) error {
	return h.srv.Shutdown(ctx)
}
