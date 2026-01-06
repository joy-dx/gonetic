package httpclient

import (
	"context"
	"fmt"
)

// StaticHeaderMiddleware injects static headers into every request.
func StaticHeaderMiddleware(headers map[string]string) Middleware {
	return func(ctx context.Context, r *HTTPRequest) error {
		if r.Headers == nil {
			r.Headers = make(map[string]string)
		}
		for k, v := range headers {
			r.Headers[k] = v
		}
		return nil
	}
}

func LoggingMiddleware(logger func(msg string)) Middleware {
	return func(ctx context.Context, r *HTTPRequest) error {
		logger(fmt.Sprintf("[HTTP] %s %s", r.Method, r.URL))
		return nil
	}
}

func InjectFieldMiddleware(key string, val any) Middleware {
	return func(ctx context.Context, r *HTTPRequest) error {
		if r.Body == nil {
			r.Body = map[string]any{}
		}
		r.Body[key] = val

		// Ensure final bytes will be recomputed from Body.
		// This is important if some earlier middleware set BodyBytes.
		r.BodyBytes = nil
		r.ContentType = "" // optional: let PrepareBody decide
		return nil
	}
}
