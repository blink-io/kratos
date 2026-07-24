package http3

import (
	khttp "github.com/go-kratos/kratos/v3/transport/http"
)

// BuildPathOption configures path construction.
type BuildPathOption = khttp.BuildPathOption

// WithQueryParams appends request fields that are not bound in the path as query parameters.
func WithQueryParams() BuildPathOption { return khttp.WithQueryParams() }

// WithOmitFields excludes fields from generated query parameters.
func WithOmitFields(fields ...string) BuildPathOption { return khttp.WithOmitFields(fields...) }

// BuildPath builds an HTTP/3 request path from a path template and request message.
// HTTP/3 paths use the same templates as HTTP/1.1 so the codec is reused.
func BuildPath(pathTemplate string, msg any, opts ...BuildPathOption) string {
	return khttp.BuildPath(pathTemplate, msg, opts...)
}
