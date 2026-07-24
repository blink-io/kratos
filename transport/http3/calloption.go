package http3

import (
	"net/http"

	khttp "github.com/go-kratos/kratos/v3/transport/http"
)

const (
	contentTypeJSON  = "application/json"
	schemeDiscovery = "discovery"
	schemeHTTPS     = "https"
)

// CallOption configures a Call before it starts or extracts information from
// a Call after it completes. HTTP/3 shares the same call semantics as
// HTTP/1.1 (request/response codec, headers, content-type negotiation), so
// the type and constructors are reused from transport/http.
type CallOption = khttp.CallOption

// EmptyCallOption does not alter the Call configuration.
type EmptyCallOption = khttp.EmptyCallOption

// CallInfo is the per-call mutable state shared with before/after hooks.
type CallInfo = khttp.CallInfo

// CsAttempt is the post-call attempt state exposed to after-call hooks.
type CsAttempt = khttp.CsAttempt

// ContentType with request content type.
func ContentType(contentType string) CallOption { return khttp.ContentType(contentType) }

// ContentTypeCallOption is BodyCallOption.
type ContentTypeCallOption = khttp.ContentTypeCallOption

// Accept sets the request Accept header.
func Accept(contentType string) CallOption { return khttp.Accept(contentType) }

// AcceptCallOption sets the accepted response content type.
type AcceptCallOption = khttp.AcceptCallOption

// Operation is serviceMethod call option.
func Operation(operation string) CallOption { return khttp.Operation(operation) }

// OperationCallOption is set ServiceMethod for client call.
type OperationCallOption = khttp.OperationCallOption

// PathTemplate is http3 path template.
func PathTemplate(pattern string) CallOption { return khttp.PathTemplate(pattern) }

// PathTemplateCallOption is set path template for client call.
type PathTemplateCallOption = khttp.PathTemplateCallOption

// Header returns a CallOptions that retrieves the http response header
// from server reply.
func Header(header *http.Header) CallOption { return khttp.Header(header) }

// HeaderCallOption is retrieve response header for client call.
type HeaderCallOption = khttp.HeaderCallOption

// defaultCallInfo returns the per-call defaults shared with HTTP/1.1.
func defaultCallInfo(path string) CallInfo {
	return khttp.DefaultCallInfo(path)
}

// Compile-time guards.
var (
	_ CallOption = EmptyCallOption{}
	_ CallOption = ContentTypeCallOption{}
	_ CallOption = AcceptCallOption{}
	_ CallOption = OperationCallOption{}
	_ CallOption = PathTemplateCallOption{}
	_ CallOption = HeaderCallOption{}
)
