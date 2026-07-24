package http3

import (
	khttp "github.com/go-kratos/kratos/v3/transport/http"
)

// Context is an HTTP/3 Context.
//
// HTTP/3 wraps the same net/http request/response model as HTTP/1.1, so the
// transport/http Context implementation is reused directly.
type Context = khttp.Context
