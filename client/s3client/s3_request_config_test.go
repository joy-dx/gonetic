package s3client

import (
	"bytes"
	"context"
	"reflect"
	"testing"
)

func TestS3RequestConfig_NewRequest_CopiesMaps_Golden(t *testing.T) {
	cases := []struct {
		name string
		in   *S3RequestConfig
		mut  func(cfg *S3RequestConfig)
		want *S3Request
	}{
		{
			name: "copies headers and extra opts (no aliasing)",
			in: &S3RequestConfig{
				Operation: "put",
				Bucket:    "b",
				Key:       "k",
				Body:      []byte("x"),
				Headers: map[string]string{
					"h1": "v1",
				},
				ExtraOpts: map[string]interface{}{
					"metadata": map[string]string{"a": "1"},
				},
			},
			mut: func(cfg *S3RequestConfig) {
				// mutate original after NewRequest; S3Request should not change.
				cfg.Headers["h1"] = "CHANGED"
				cfg.Headers["h2"] = "v2"
				cfg.ExtraOpts["new"] = "x"
			},
			want: &S3Request{
				Operation: "put",
				Bucket:    "b",
				Key:       "k",
				Body:      []byte("x"),
				Headers:   map[string]string{"h1": "v1"},
				ExtraOpts: map[string]any{
					"metadata": map[string]string{"a": "1"},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reqAny, err := tc.in.NewRequest(context.Background())
			if err != nil {
				t.Fatalf("NewRequest error: %v", err)
			}
			req, ok := reqAny.(*S3Request)
			if !ok {
				t.Fatalf("expected *S3Request, got %T", reqAny)
			}

			// mutate input config after building request
			if tc.mut != nil {
				tc.mut(tc.in)
			}

			if req.Operation != tc.want.Operation ||
				req.Bucket != tc.want.Bucket ||
				req.Key != tc.want.Key ||
				!bytes.Equal(req.Body, tc.want.Body) {
				t.Fatalf("scalar mismatch: got=%+v want=%+v", req, tc.want)
			}

			if !reflect.DeepEqual(req.Headers, tc.want.Headers) {
				t.Fatalf("headers mismatch: got=%v want=%v", req.Headers, tc.want.Headers)
			}

			// Top-level container should be present with original value.
			if _, ok := req.ExtraOpts["metadata"]; !ok {
				t.Fatalf("expected ExtraOpts[\"metadata\"] to be present")
			}
		})
	}
}

func TestS3RequestConfig_Ref_Golden(t *testing.T) {
	cfg := &S3RequestConfig{}
	if cfg.Ref() != NetClientS3Ref {
		t.Fatalf("Ref mismatch: got=%v want=%v", cfg.Ref(), NetClientS3Ref)
	}
}
