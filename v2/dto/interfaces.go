package dto

import (
	"context"
)

type NetInterface interface {
	Hydrate(ctx context.Context) error
	State() *NetState
	DownloadFile(ctx context.Context, cfg *DownloadFileConfig) (string, error)
	Get(ctx context.Context, url string, withRetry bool) (Response, error)
	Post(ctx context.Context, url string, payload map[string]interface{}, withRetry bool) (Response, error)
	RegisterClient(ref string, client NetClientInterface)
	RequestOnce(ctx context.Context, cfg *RequestConfig) (Response, error)
	RequestWithRetry(ctx context.Context, cfg *RequestConfig) (Response, error)
}

// AuthProvider defines methods for non-OAuth authentication schemes.
// Returned dto.TokenInfo may include cookies or access tokens.
// The *http.dto.Response allows cookie extraction.
type AuthProvider interface {
	Authenticate(ctx context.Context) (TokenInfo, error)
	Refresh(ctx context.Context, old TokenInfo) (TokenInfo, error)
}

// Middleware is executed before each request.
// Returning nil continues the chain; returning an error aborts it.
type Middleware func(ctx context.Context, cfg *RequestConfig) error

type HTTPMiddleware func(ctx context.Context, req any) error
type S3Middleware func(ctx context.Context, req any) error

// HTTPClient abstracts http.Client for mocking
type NetClientInterface interface {
	Ref() string
	Type() NetClientType
	ProcessRequest(ctx context.Context, cfg *RequestConfig) (Response, error)
}
