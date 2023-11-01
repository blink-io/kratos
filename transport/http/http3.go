package http

func EnableHttp3() ServerOption {
	return func(s *Server) {
		s.enableHttp3 = true
	}
}
