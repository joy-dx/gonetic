package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/joy-dx/gonetic/dto"
	"golang.org/x/oauth2"
)

func Test_Middlewares_golden(t *testing.T) {
	type golden struct {
		wantBody       string
		wantHeaderKV   map[string]string
		wantAuthPrefix string
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// just echo ok
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	cfg := DefaultHTTPClientConfig()
	cfg.WithMiddleware(
		StaticHeaderMiddleware(map[string]string{
			"X-Static": "1",
		}),
		InjectFieldMiddleware("injected", "yes"),
	)

	// Provide OAuth so attachAuth is exercised
	ts := &staticTokenSource{
		tok: &oauth2.Token{
			AccessToken: "abc",
			TokenType:   "bearer",
			Expiry:      time.Now().Add(1 * time.Hour),
		},
	}
	cfg.OAuthSource = ts

	c := newTestClient(t, &cfg)

	reqCfg := DefaultHTTPRequestConfig()
	reqCfg.WithMethod(http.MethodPost).
		WithURL(srv.URL).
		WithBody(map[string]any{"orig": "v"}).
		WithHeaders(map[string]string{
			"X-FromSpec": "1",
		})

	gotResp, err := c.ProcessRequest(context.Background(), &dto.RequestConfig{
		ReqConfig: &reqCfg,
	})
	if err != nil {
		t.Fatalf("ProcessRequest error: %v", err)
	}
	if gotResp.StatusCode != 200 {
		t.Fatalf("status=%d; want 200", gotResp.StatusCode)
	}

	// NOTE: we cannot directly read the built request here without replacing Transport,
	// so we verify through server side by checking expected behaviors using a recording server
	// in the next test. This test ensures the chain doesn't error and OAuth was fetched.
	if ts.n.Load() != 1 {
		t.Fatalf("oauth Token() calls=%d; want 1", ts.n.Load())
	}
}
