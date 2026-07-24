package http3

import (
	"net/http"

	"google.golang.org/genproto/googleapis/api/httpbody"

	"github.com/go-kratos/kratos/v3/encoding"
	khttp "github.com/go-kratos/kratos/v3/transport/http"
)

// HTTP/3 shares the same wire codecs as HTTP/1.1 and HTTP/2: the request and
// response bodies carry the same headers, the same content-type negotiation
// and the same body framing. The codec logic in transport/http (built on
// net/http, encoding.Codec and chi routing) is therefore reused verbatim.

// DecodeRequestFunc is decode request func.
type DecodeRequestFunc = khttp.DecodeRequestFunc

// EncodeResponseFunc is encode response func.
type EncodeResponseFunc = khttp.EncodeResponseFunc

// EncodeErrorFunc is encode error func.
type EncodeErrorFunc = khttp.EncodeErrorFunc

// Redirector replies to the request with a redirect to url
// which may be a path relative to the request path.
type Redirector = khttp.Redirector

// Request type net/http.
type Request = http.Request

// ResponseWriter type net/http.
type ResponseWriter = http.ResponseWriter

// Flusher type net/http
type Flusher = http.Flusher

// supportPackageIsVersion3 is referenced from generated code.
const supportPackageIsVersion3 = khttp.SupportPackageIsVersion3

// DefaultRequestVars decodes the request vars to object.
func DefaultRequestVars(r *http.Request, v any) error {
	return khttp.DefaultRequestVars(r, v)
}

// DefaultRequestQuery decodes the request vars to object.
func DefaultRequestQuery(r *http.Request, v any) error {
	return khttp.DefaultRequestQuery(r, v)
}

// DefaultRequestDecoder decodes the request body to object.
func DefaultRequestDecoder(r *http.Request, v any) error {
	return khttp.DefaultRequestDecoder(r, v)
}

// DefaultResponseEncoder encodes the object to the HTTP response.
func DefaultResponseEncoder(w http.ResponseWriter, r *http.Request, v any) error {
	return khttp.DefaultResponseEncoder(w, r, v)
}

// DefaultErrorEncoder encodes the error to the HTTP response.
func DefaultErrorEncoder(w http.ResponseWriter, r *http.Request, err error) {
	khttp.DefaultErrorEncoder(w, r, err)
}

// CodecForRequest get encoding.Codec via http.Request.
func CodecForRequest(r *http.Request, name string) (encoding.Codec, bool) {
	return khttp.CodecForRequest(r, name)
}

// BodyContentType returns the content type carried by v or a binary default.
func BodyContentType(v any) string {
	return khttp.BodyContentType(v)
}

// httpBody extracts a *google.api.HttpBody from v when v holds one.
func httpBody(v any) (*httpbody.HttpBody, bool) {
	return khttp.HTTPBody(v)
}
