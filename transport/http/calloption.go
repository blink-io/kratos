package http

import (
	"net/http"
)

const (
	contentTypeJSON = "application/json"
	schemeDiscovery = "discovery"
	schemeHTTP      = "http"
	schemeHTTPS     = "https"
)

// CallOption configures a Call before it starts or extracts information from
// a Call after it completes.
type CallOption interface {
	// Before is called before the call is sent to any server. If Before
	// returns a non-nil error, the RPC fails with that error.
	Before(*CallInfo) error

	// After is called after the call has completed. After cannot return an
	// error, so any failures should be reported via output parameters.
	After(*CallInfo, *CsAttempt)
}

// CallInfo is the per-call mutable state shared with before/after hooks.
// It is exported so alternative transport implementations (e.g. HTTP/3) can
// reuse the same call option semantics.
type CallInfo struct {
	ContentType    string
	ContentTypeSet bool
	Accept         string
	Operation      string
	PathTemplate   string
	HeaderCarrier  *http.Header
}

// EmptyCallOption does not alter the Call configuration.
// It can be embedded in another structure to carry satellite data for use
// by interceptors.
type EmptyCallOption struct{}

func (EmptyCallOption) Before(*CallInfo) error      { return nil }
func (EmptyCallOption) After(*CallInfo, *CsAttempt) {}

// CsAttempt is the post-call attempt state exposed to after-call hooks.
type CsAttempt struct {
	Res *http.Response
}

// ContentType with request content type.
func ContentType(contentType string) CallOption {
	return ContentTypeCallOption{ContentType: contentType}
}

// ContentTypeCallOption is BodyCallOption
type ContentTypeCallOption struct {
	EmptyCallOption
	ContentType string
}

func (o ContentTypeCallOption) Before(c *CallInfo) error {
	c.ContentType = o.ContentType
	c.ContentTypeSet = true
	return nil
}

// Accept sets the request Accept header.
func Accept(contentType string) CallOption {
	return AcceptCallOption{ContentType: contentType}
}

// AcceptCallOption sets the accepted response content type.
type AcceptCallOption struct {
	EmptyCallOption
	ContentType string
}

func (o AcceptCallOption) Before(c *CallInfo) error {
	c.Accept = o.ContentType
	return nil
}

// DefaultCallInfo returns a CallInfo populated with defaults shared by both
// HTTP/1.1 and HTTP/3 transports.
func DefaultCallInfo(path string) CallInfo {
	return CallInfo{
		ContentType:  contentTypeJSON,
		Operation:    path,
		PathTemplate: path,
	}
}

// Operation is serviceMethod call option
func Operation(operation string) CallOption {
	return OperationCallOption{Operation: operation}
}

// OperationCallOption is set ServiceMethod for client call
type OperationCallOption struct {
	EmptyCallOption
	Operation string
}

func (o OperationCallOption) Before(c *CallInfo) error {
	c.Operation = o.Operation
	return nil
}

// PathTemplate is http path template
func PathTemplate(pattern string) CallOption {
	return PathTemplateCallOption{Pattern: pattern}
}

// PathTemplateCallOption is set path template for client call
type PathTemplateCallOption struct {
	EmptyCallOption
	Pattern string
}

func (o PathTemplateCallOption) Before(c *CallInfo) error {
	c.PathTemplate = o.Pattern
	return nil
}

// Header returns a CallOptions that retrieves the http response header
// from server reply.
func Header(header *http.Header) CallOption {
	return HeaderCallOption{Header: header}
}

// HeaderCallOption is retrieve response header for client call
type HeaderCallOption struct {
	EmptyCallOption
	Header *http.Header
}

func (o HeaderCallOption) Before(c *CallInfo) error {
	c.HeaderCarrier = o.Header
	return nil
}

func (o HeaderCallOption) After(_ *CallInfo, cs *CsAttempt) {
	if cs.Res != nil && cs.Res.Header != nil {
		*o.Header = cs.Res.Header
	}
}
