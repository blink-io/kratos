// Package status provides HTTP/3 status code conversion utilities.
//
// HTTP/3 returns the same numeric status codes as HTTP/1.1 and HTTP/2, so the
// transport/http/status package is reused directly.
package status

import (
	"net/http"

	"google.golang.org/grpc/codes"

	khttpstatus "github.com/go-kratos/kratos/v3/transport/http/status"
)

// ClientClosed is non-standard http status code, which defined by nginx.
const ClientClosed = khttpstatus.ClientClosed

// Converter is a status converter.
type Converter = khttpstatus.Converter

// DefaultConverter default converter.
var DefaultConverter = khttpstatus.DefaultConverter

// ToGRPCCode converts an HTTP error code into the corresponding gRPC response status.
func ToGRPCCode(code int) codes.Code { return khttpstatus.ToGRPCCode(code) }

// FromGRPCCode converts a gRPC error code into the corresponding HTTP response status.
func FromGRPCCode(code codes.Code) int { return khttpstatus.FromGRPCCode(code) }

// Ensure net/http is referenced for downstream imports.
var _ = http.StatusOK
