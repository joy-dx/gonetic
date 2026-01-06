package httpclient

import (
	"context"
	"net/http"

	"github.com/joy-dx/gonetic/dto"
)

// HTTPRequestConfig is immutable input (safe to reuse).
type HTTPRequestConfig struct {
	Method string `json:"method" yaml:"method"`
	URL    string
	Body   map[string]interface{} `json:"body" yaml:"body"`
	// BodyType application/json, application/x-www-form-urlencoded
	BodyType string            `json:"body_type" yaml:"body_type"`
	Headers  map[string]string `json:"headers" yaml:"headers"`
}

func DefaultHTTPRequestConfig() HTTPRequestConfig {
	return HTTPRequestConfig{
		Method:   http.MethodGet,
		Body:     map[string]interface{}{},
		BodyType: "application/json",
		Headers:  make(map[string]string),
	}
}

func (c *HTTPRequestConfig) Ref() dto.NetClientType {
	return NetClientHTTPRef
}

func (c *HTTPRequestConfig) WithMethod(method string) *HTTPRequestConfig {
	c.Method = method
	return c
}
func (c *HTTPRequestConfig) WithBody(body map[string]interface{}) *HTTPRequestConfig {
	c.Body = body
	return c
}
func (c *HTTPRequestConfig) WithHeaders(headers map[string]string) *HTTPRequestConfig {
	c.Headers = headers
	return c
}
func (c *HTTPRequestConfig) WithURL(url string) *HTTPRequestConfig {
	c.URL = url
	return c
}

// NewRequest creates a per-call mutable request object.
// This avoids mutating the spec and avoids leaks without cloning the spec maps.
func (c *HTTPRequestConfig) NewRequest(ctx context.Context) (any, error) {
	r := &HTTPRequest{
		Method:   c.Method,
		URL:      c.URL,
		BodyType: c.BodyType,
		Headers:  make(map[string]string, len(c.Headers)),
		Body:     make(map[string]any, len(c.Body)),
	}
	for k, v := range c.Headers {
		r.Headers[k] = v
	}
	for k, v := range c.Body {
		r.Body[k] = v
	}
	return r, nil
}

// HTTPRequest is per-call mutable state.
type HTTPRequest struct {
	Method   string
	URL      string
	Body     map[string]any
	BodyType string
	Headers  map[string]string
	// Finalized wire body (deterministic for tests and retries)
	BodyBytes   []byte
	ContentType string
}

func (r *HTTPRequest) ClientType() dto.NetClientType { return NetClientHTTPRef }

func (r *HTTPRequest) SetHeader(k, v string) {
	if r.Headers == nil {
		r.Headers = map[string]string{}
	}
	r.Headers[k] = v
}

func (r *HTTPRequest) Header(k string) string {
	if r.Headers == nil {
		return ""
	}
	return r.Headers[k]
}
