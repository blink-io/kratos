package adapter

import (
	"crypto/tls"
	"net/http"

	"github.com/quic-go/quic-go/http3"
	"golang.org/x/net/context"
)

var _ ServerAdapter = (*Http3Adapter)(nil)

type Http3Adapter struct {
	srv *http3.Server
}

func NewHttp3Adapter(ctx context.Context, h http.Handler, tlsConf *tls.Config) ServerAdapter {
	srv := &http3.Server{
		Handler:   h,
		TLSConfig: tlsConf,
	}
	srv.ListenAndServe()
	adp := &Http3Adapter{
		srv: srv,
	}
	return adp
}

func (h *Http3Adapter) Handler() http.Handler {
	//TODO implement me
	panic("implement me")
}

func (h *Http3Adapter) Shutdown(ctx context.Context) error {
	//TODO implement me
	panic("implement me")
}
