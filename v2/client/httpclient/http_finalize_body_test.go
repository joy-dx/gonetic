package httpclient

import (
	"reflect"
	"strings"
	"testing"
)

func Test_HTTPRequest_FinalizeBody_golden(t *testing.T) {
	type golden struct {
		bodyBytes   []byte
		contentType string
	}
	type tc struct {
		name string
		req  HTTPRequest
		want golden
		err  string
	}

	cases := []tc{
		{
			name: "json body builds bytes and sets content-type",
			req: HTTPRequest{
				Body:     map[string]any{"a": "b"},
				BodyType: "application/json",
			},
			want: golden{
				bodyBytes:   mustJSON(t, map[string]any{"a": "b"}),
				contentType: "application/json",
			},
		},
		{
			name: "form body encodes and sets content-type",
			req: HTTPRequest{
				Body:     map[string]any{"a": "b", "n": 123},
				BodyType: "application/x-www-form-urlencoded",
			},
			// url.Values encoding order is stable for single entries but not guaranteed
			// across maps; so we assert using contains below for bodyBytes in this case.
			want: golden{
				contentType: "application/x-www-form-urlencoded",
			},
		},
		{
			name: "nil body returns nil bytes and empty content-type",
			req: HTTPRequest{
				Body:     nil,
				BodyType: "application/json",
			},
			want: golden{
				bodyBytes:   nil,
				contentType: "",
			},
		},
		{
			name: "unsupported body type errors",
			req: HTTPRequest{
				Body:     map[string]any{"a": "b"},
				BodyType: "text/plain",
			},
			err: "unsupported body_type",
		},
		{
			name: "if BodyBytes already set, do not overwrite",
			req: HTTPRequest{
				Body:        map[string]any{"a": "b"},
				BodyType:    "application/json",
				BodyBytes:   []byte("raw"),
				ContentType: "",
			},
			want: golden{
				bodyBytes:   []byte("raw"),
				contentType: "",
			},
		},
		{
			name: "if BodyBytes set and ContentType set, keep both",
			req: HTTPRequest{
				BodyBytes:   []byte("raw"),
				ContentType: "application/custom",
			},
			want: golden{
				bodyBytes:   []byte("raw"),
				contentType: "application/custom",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.req.FinalizeBody()
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("FinalizeBody err=%v; want contains %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("FinalizeBody unexpected error: %v", err)
			}

			if c.name == "form body encodes and sets content-type" {
				s := string(c.req.BodyBytes)
				if !(strings.Contains(s, "a=b") && strings.Contains(s, "n=123")) {
					t.Fatalf("form encoding=%q; want contains a=b and n=123", s)
				}
			} else if !reflect.DeepEqual(c.req.BodyBytes, c.want.bodyBytes) {
				t.Fatalf("BodyBytes=%q; want %q", c.req.BodyBytes, c.want.bodyBytes)
			}

			if c.req.ContentType != c.want.contentType {
				t.Fatalf("ContentType=%q; want %q", c.req.ContentType, c.want.contentType)
			}
		})
	}
}
