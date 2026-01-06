package httpclient

import (
	"context"
	"time"

	"github.com/joy-dx/gonetic/dto"
	"golang.org/x/oauth2"
)

type Middleware func(ctx context.Context, req *HTTPRequest) error

type HTTPClientConfig struct {
	AuthProvider  dto.AuthProvider
	OAuthSource   oauth2.TokenSource
	RefreshBuffer time.Duration
	Middlewares   []Middleware
}

func DefaultHTTPClientConfig() HTTPClientConfig {
	return HTTPClientConfig{
		RefreshBuffer: 30 * time.Second,
		Middlewares:   make([]Middleware, 0),
	}
}

// WithRefreshBuffer sets the early-refresh buffer.
func (c *HTTPClientConfig) WithAuthProvider(provider dto.AuthProvider) *HTTPClientConfig {
	c.AuthProvider = provider
	return c
}
func (c *HTTPClientConfig) WithOAuthSource(tokenSource oauth2.TokenSource) *HTTPClientConfig {
	c.OAuthSource = tokenSource
	return c
}
func (c *HTTPClientConfig) WithRefreshBuffer(d time.Duration) *HTTPClientConfig {
	c.RefreshBuffer = d
	return c
}
func (c *HTTPClientConfig) WithMiddleware(m ...Middleware) *HTTPClientConfig {
	c.Middlewares = append(c.Middlewares, m...)
	return c
}
