package quic

import (
	"context"
	"net"

	"github.com/quic-go/quic-go/http3"
)

type Server struct {
	*http3.Server
	BaseContext func(net.Listener) context.Context
}

func (s *Server) Serve(lis net.Listener) error {
	return nil
}

func (s *Server) Shutdown(ctx context.Context) {
	s.Close()
}
