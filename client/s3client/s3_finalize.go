package s3client

import (
	"bytes"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Finalize builds the deterministic AWS SDK input struct for the operation.
// Call this exactly once after middleware has run and before executing.
func (r *S3Request) Finalize() error {
	// Clear any previous prepared state (in case caller reuses r incorrectly).
	r.PutInput = nil
	r.GetInput = nil
	r.DeleteInput = nil
	r.ListInput = nil

	switch r.Operation {
	case "get":
		r.GetInput = &s3.GetObjectInput{
			Bucket: aws.String(r.Bucket),
			Key:    aws.String(r.Key),
		}
		// Note: headers/customizations for GetObject are limited. If you want
		// Range, IfMatch, SSE-C, etc, add explicit fields to S3Request rather
		// than trying to pipe via Headers/ExtraOpts.
		return nil

	case "put":
		in := &s3.PutObjectInput{
			Bucket: aws.String(r.Bucket),
			Key:    aws.String(r.Key),
			Body:   bytes.NewReader(r.Body),
		}
		if r.ContentType != "" {
			in.ContentType = aws.String(r.ContentType)
		}

		// Apply metadata from ExtraOpts in a deterministic way.
		// Convention: ExtraOpts["metadata"] can be map[string]string or map[string]any.
		if md, ok := extractStringMap(r.ExtraOpts, "metadata"); ok && len(md) > 0 {
			in.Metadata = md
		}

		// Optionally support common top-level options too (example: cache control).
		// Convention: ExtraOpts["cache_control"] string
		if v, ok := r.ExtraOpts["cache_control"].(string); ok && v != "" {
			in.CacheControl = aws.String(v)
		}

		r.PutInput = in
		return nil

	case "delete":
		r.DeleteInput = &s3.DeleteObjectInput{
			Bucket: aws.String(r.Bucket),
			Key:    aws.String(r.Key),
		}
		return nil

	case "list":
		r.ListInput = &s3.ListObjectsV2Input{
			Bucket: aws.String(r.Bucket),
		}
		if r.Prefix != "" {
			r.ListInput.Prefix = aws.String(r.Prefix)
		}
		return nil

	default:
		return fmt.Errorf("unsupported s3 operation: %s", r.Operation)
	}
}

// extractStringMap reads ExtraOpts[key] as either map[string]string or map[string]any
// with string values, returning a map[string]string.
func extractStringMap(extra map[string]any, key string) (map[string]string, bool) {
	raw, ok := extra[key]
	if !ok || raw == nil {
		return nil, false
	}

	switch v := raw.(type) {
	case map[string]string:
		// Make a copy to prevent later mutations impacting prepared input.
		out := make(map[string]string, len(v))
		for k, s := range v {
			out[k] = s
		}
		return out, true

	case map[string]any:
		out := make(map[string]string, len(v))
		for k, val := range v {
			s, ok := val.(string)
			if !ok {
				continue
			}
			out[k] = s
		}
		return out, true

	default:
		return nil, false
	}
}
