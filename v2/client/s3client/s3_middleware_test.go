package s3client

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

func TestStaticS3MetaMiddleware_Golden(t *testing.T) {
	cases := []struct {
		name string
		req  *S3Request
		meta map[string]string
		want map[string]any
	}{
		{
			name: "no-op for non-put",
			req: &S3Request{
				Operation: "get",
				ExtraOpts: map[string]any{},
			},
			meta: map[string]string{"a": "1"},
			want: map[string]any{},
		},
		{
			name: "creates ExtraOpts and metadata map when missing",
			req: &S3Request{
				Operation: "put",
			},
			meta: map[string]string{"a": "1"},
			want: map[string]any{
				"metadata": map[string]string{"a": "1"},
			},
		},
		{
			name: "merges into existing metadata map",
			req: &S3Request{
				Operation: "put",
				ExtraOpts: map[string]any{
					"metadata": map[string]string{"a": "old", "keep": "y"},
				},
			},
			meta: map[string]string{"a": "new", "b": "2"},
			want: map[string]any{
				"metadata": map[string]string{"a": "new", "b": "2", "keep": "y"},
			},
		},
		{
			name: "if metadata exists but is wrong type, replaces with new map",
			req: &S3Request{
				Operation: "put",
				ExtraOpts: map[string]any{
					"metadata": "nope",
				},
			},
			meta: map[string]string{"a": "1"},
			want: map[string]any{
				"metadata": map[string]string{"a": "1"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mw := StaticS3MetaMiddleware(tc.meta)
			if err := mw(context.Background(), tc.req); err != nil {
				t.Fatalf("middleware error: %v", err)
			}
			if !reflect.DeepEqual(tc.req.ExtraOpts, tc.want) {
				t.Fatalf("ExtraOpts mismatch:\n got=%#v\nwant=%#v", tc.req.ExtraOpts, tc.want)
			}
		})
	}
}

func TestLoggingMiddleware_Format_Golden(t *testing.T) {
	var got string
	mw := LoggingMiddleware(func(msg string) { got = msg })

	r := &S3Request{
		Operation: "put",
		Bucket:    "bucket",
		Key:       "key",
	}
	if err := mw(context.Background(), r); err != nil {
		t.Fatalf("middleware error: %v", err)
	}

	want := "[S3] PUT s3://bucket/key"
	if got != want {
		t.Fatalf("log format mismatch:\n got=%q\nwant=%q", got, want)
	}
}

func ExampleStaticS3MetaMiddleware() {
	r := &S3Request{Operation: "put"}
	_ = StaticS3MetaMiddleware(map[string]string{"a": "1"})(context.Background(), r)
	fmt.Println(r.ExtraOpts["metadata"])
	// Output: map[a:1]
}
