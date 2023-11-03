package http

import (
	"crypto/tls"
	"net/http"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

func EnableHTTP3() ServerOption {
	return func(s *Server) {
		s.enableHttp3 = true
	}
}

// HTTP3Listener with http3 server listener
func HTTP3Listener(lis http3.QUICEarlyListener) ServerOption {
	return func(s *Server) {
		s.http3Lis = lis
	}
}

func HTTP3RoundTripper(tlsConf *tls.Config, qconf *quic.Config) http.RoundTripper {
	roundTripper := &http3.RoundTripper{
		TLSClientConfig: tlsConf,
		QuicConfig:      qconf,
	}
	return roundTripper
}
