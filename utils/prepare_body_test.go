package utils

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestPrepareBody_Golden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		body        map[string]interface{}
		bodyType    string
		wantCT      string
		wantBody    string
		wantNilBody bool
		wantErr     bool
	}{
		{
			name:        "nil body returns nil,nil",
			body:        nil,
			bodyType:    "application/json",
			wantNilBody: true,
		},
		{
			name:     "json marshals",
			body:     map[string]interface{}{"a": 1, "b": "x"},
			bodyType: "application/json",
			wantCT:   "application/json",
			// Map order is not stable; validate via JSON equivalence below.
			wantBody: `{"a":1,"b":"x"}`,
		},
		{
			name:     "form urlencoded encodes",
			body:     map[string]interface{}{"a": 1, "b": "x"},
			bodyType: "application/x-www-form-urlencoded",
			wantCT:   "application/x-www-form-urlencoded",
			// url.Values Encode sorts by key -> a=1&b=x
			wantBody: "a=1&b=x",
		},
		{
			name:     "bodyType case-insensitive",
			body:     map[string]interface{}{"a": "b"},
			bodyType: "Application/JSON",
			wantCT:   "application/json",
			wantBody: `{"a":"b"}`,
		},
		{
			name:     "unsupported body type errors",
			body:     map[string]interface{}{"a": 1},
			bodyType: "text/plain",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, gotCT, err := PrepareBody(tt.body, tt.bodyType)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}

			if tt.wantNilBody {
				if got != nil || gotCT != "" || err != nil {
					t.Fatalf("expected nil result, got body=%v ct=%q err=%v", got, gotCT, err)
				}
				return
			}

			if gotCT != tt.wantCT {
				t.Fatalf("content-type=%q want %q", gotCT, tt.wantCT)
			}

			if tt.bodyType == "application/json" || tt.bodyType == "Application/JSON" {
				// JSON compare without ordering concerns:
				if !jsonEqual(t, got, []byte(tt.wantBody)) {
					t.Fatalf("json not equal: got=%s want=%s", got, tt.wantBody)
				}
				return
			}

			if !bytes.Equal(got, []byte(tt.wantBody)) {
				t.Fatalf("body=%q want %q", string(got), tt.wantBody)
			}
		})
	}
}

// jsonEqual compares JSON documents regardless of key order.
func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()

	var o1 any
	var o2 any
	if err := json.Unmarshal(a, &o1); err != nil {
		t.Fatalf("unmarshal a: %v", err)
	}
	if err := json.Unmarshal(b, &o2); err != nil {
		t.Fatalf("unmarshal b: %v", err)
	}
	return reflect.DeepEqual(o1, o2)
}
