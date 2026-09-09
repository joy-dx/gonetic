package s3client

import (
	"context"
	"fmt"
	"strings"
)

// StaticS3MetaMiddleware adds default metadata to each S3 put operation.
func StaticS3MetaMiddleware(meta map[string]string) Middleware {
	return func(ctx context.Context, r *S3Request) error {
		if r.Operation != "put" {
			return nil
		}
		if r.ExtraOpts == nil {
			r.ExtraOpts = map[string]any{}
		}

		// Ensure metadata container exists
		mdAny, ok := r.ExtraOpts["metadata"]
		var md map[string]string
		if ok {
			if existing, ok := mdAny.(map[string]string); ok {
				md = existing
			}
		}
		if md == nil {
			md = make(map[string]string)
		}

		for k, v := range meta {
			md[k] = v
		}

		r.ExtraOpts["metadata"] = md
		return nil
	}
}

func LoggingMiddleware(logger func(msg string)) Middleware {
	return func(ctx context.Context, r *S3Request) error {
		logger(fmt.Sprintf(
			"[S3] %s s3://%s/%s",
			strings.ToUpper(r.Operation),
			r.Bucket,
			r.Key,
		))
		return nil
	}
}
