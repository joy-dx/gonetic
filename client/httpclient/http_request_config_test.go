package httpclient

import (
	"context"
	"net/http"
	"testing"
)

func Test_HTTPRequestConfig_NewRequest_clonesMaps(t *testing.T) {
	cfg := DefaultHTTPRequestConfig()
	cfg.Method = http.MethodPost
	cfg.URL = "http://example.com"
	cfg.BodyType = "application/json"
	cfg.Headers["X-A"] = "1"
	cfg.Body["k"] = "v"

	anyReq, err := cfg.NewRequest(context.Background())
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}

	req := anyReq.(*HTTPRequest)

	// Mutate request maps and ensure config maps remain unchanged.
	req.Headers["X-A"] = "2"
	req.Headers["X-B"] = "3"
	req.Body["k"] = "vv"
	req.Body["k2"] = "v2"

	if cfg.Headers["X-A"] != "1" || cfg.Headers["X-B"] != "" {
		t.Fatalf("headers were not cloned: cfg.Headers=%v", cfg.Headers)
	}
	if cfg.Body["k"] != "v" || cfg.Body["k2"] != nil {
		t.Fatalf("body was not cloned: cfg.Body=%v", cfg.Body)
	}
}
