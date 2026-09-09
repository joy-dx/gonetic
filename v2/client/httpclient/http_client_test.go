package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joy-dx/gonetic/config"
	"github.com/joy-dx/gonetic/dto"
	"golang.org/x/oauth2"
)

// --- helpers ----------------------------------------------------------------

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

func newTestClient(t *testing.T, cfg *HTTPClientConfig) *HTTPClient {
	t.Helper()

	netCfg := &config.NetSvcConfig{
		RequestTimeout: 2 * time.Second,
	}
	if cfg == nil {
		c := DefaultHTTPClientConfig()
		cfg = &c
	}
	return NewHTTPClient("test", netCfg, cfg)
}

type staticTokenSource struct {
	tok *oauth2.Token
	err error
	n   atomic.Int64
}

func (s *staticTokenSource) Token() (*oauth2.Token, error) {
	s.n.Add(1)
	if s.err != nil {
		return nil, s.err
	}
	// return a copy to avoid tests mutating shared state
	cpy := *s.tok
	return &cpy, nil
}

type fakeAuthProvider struct {
	authenticate func(ctx context.Context) (dto.TokenInfo, error)
	refresh      func(ctx context.Context, old dto.TokenInfo) (dto.TokenInfo, error)
}

func (f fakeAuthProvider) Authenticate(ctx context.Context) (dto.TokenInfo, error) {
	if f.authenticate == nil {
		return dto.TokenInfo{}, errors.New("Authenticate not implemented")
	}
	return f.authenticate(ctx)
}

func (f fakeAuthProvider) Refresh(ctx context.Context, old dto.TokenInfo) (dto.TokenInfo, error) {
	if f.refresh == nil {
		return dto.TokenInfo{}, errors.New("Refresh not implemented")
	}
	return f.refresh(ctx, old)
}

type recordedRequest struct {
	Method      string
	Path        string
	Header      http.Header
	Body        []byte
	ContentType string
}

func newRecordingServer(t *testing.T, handler func(rr recordedRequest, w http.ResponseWriter)) (*httptest.Server, *recordedRequest) {
	t.Helper()

	var last recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := ioReadAll(r.Body)
		_ = r.Body.Close()

		last = recordedRequest{
			Method:      r.Method,
			Path:        r.URL.Path,
			Header:      r.Header.Clone(),
			Body:        b,
			ContentType: r.Header.Get("Content-Type"),
		}
		handler(last, w)
	}))
	return srv, &last
}

func ioReadAll(rc io.ReadCloser) ([]byte, error) {
	if rc == nil {
		return nil, nil
	}
	defer func() { _ = rc.Close() }()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return []byte(sb.String()), err
			}
			if errors.Is(err, http.ErrBodyReadAfterClose) {
				return []byte(sb.String()), err
			}
			// io.EOF ends the loop
			break
		}
	}
	return []byte(sb.String()), nil
}

// --- tests ------------------------------------------------------------------

func Test_HTTPClient_ProcessRequest_golden_endToEnd(t *testing.T) {
	type golden struct {
		status int
		body   string

		wantReqMethod string
		wantAuth      string
		wantCookie    string
		wantHeaders   map[string]string
		wantCT        string
		wantBodyJSON  map[string]any

		// cookie capture
		expectStoredCookieName string

		errContains string
	}

	makeServer := func(t *testing.T, g golden) (*httptest.Server, *recordedRequest) {
		t.Helper()
		return newRecordingServer(t, func(rr recordedRequest, w http.ResponseWriter) {
			// simulate Set-Cookie if requested
			if g.expectStoredCookieName != "" {
				http.SetCookie(w, &http.Cookie{
					Name:  g.expectStoredCookieName,
					Value: "cookieval",
					Path:  "/",
				})
			}

			// unauthorized scenario
			if g.status != 0 {
				w.WriteHeader(g.status)
			} else {
				w.WriteHeader(200)
			}
			if g.body != "" {
				_, _ = w.Write([]byte(g.body))
			}
		})
	}

	cases := []struct {
		name  string
		setup func(t *testing.T) (*HTTPClient, *httptest.Server, *recordedRequest, golden)
	}{
		{
			name: "oauth bearer attached + static headers + json body",
			setup: func(t *testing.T) (*HTTPClient, *httptest.Server, *recordedRequest, golden) {
				g := golden{
					status:        200,
					body:          "ok",
					wantReqMethod: http.MethodPost,
					wantAuth:      "Bearer abc",
					wantHeaders: map[string]string{
						"X-Static":   "1",
						"X-FromSpec": "1",
					},
					wantCT:       "application/json",
					wantBodyJSON: map[string]any{"orig": "v"},
				}

				srv, last := makeServer(t, g)

				cfg := DefaultHTTPClientConfig()
				cfg.OAuthSource = &staticTokenSource{
					tok: &oauth2.Token{
						AccessToken: "abc",
						TokenType:   "bearer",
						Expiry:      time.Now().Add(1 * time.Hour),
					},
				}
				cfg.WithMiddleware(StaticHeaderMiddleware(map[string]string{
					"X-Static": "1",
				}))

				c := newTestClient(t, &cfg)
				return c, srv, last, g
			},
		},
		{
			name: "authprovider used when no oauth + inject field middleware recomputes bytes",
			setup: func(t *testing.T) (*HTTPClient, *httptest.Server, *recordedRequest, golden) {
				g := golden{
					status:        200,
					body:          "ok",
					wantReqMethod: http.MethodPost,
					wantAuth:      "Bearer from-provider",
					wantCT:        "application/json",
					wantBodyJSON:  map[string]any{"orig": "v", "injected": "yes"},
				}

				srv, last := makeServer(t, g)

				cfg := DefaultHTTPClientConfig()
				cfg.AuthProvider = fakeAuthProvider{
					authenticate: func(ctx context.Context) (dto.TokenInfo, error) {
						return dto.TokenInfo{
							AccessToken: "from-provider",
							TokenType:   "bearer",
							Expiry:      time.Now().Add(1 * time.Hour),
						}, nil
					},
				}
				cfg.WithMiddleware(InjectFieldMiddleware("injected", "yes"))

				c := newTestClient(t, &cfg)
				return c, srv, last, g
			},
		},
		{
			name: "oauth takes precedence over authprovider",
			setup: func(t *testing.T) (*HTTPClient, *httptest.Server, *recordedRequest, golden) {
				g := golden{
					status:        200,
					body:          "ok",
					wantReqMethod: http.MethodGet,
					wantAuth:      "Bearer oauth",
				}
				srv, last := makeServer(t, g)

				cfg := DefaultHTTPClientConfig()
				cfg.OAuthSource = &staticTokenSource{
					tok: &oauth2.Token{
						AccessToken: "oauth",
						TokenType:   "bearer",
						Expiry:      time.Now().Add(1 * time.Hour),
					},
				}
				cfg.AuthProvider = fakeAuthProvider{
					authenticate: func(ctx context.Context) (dto.TokenInfo, error) {
						return dto.TokenInfo{
							AccessToken: "provider",
							TokenType:   "bearer",
							Expiry:      time.Now().Add(1 * time.Hour),
						}, nil
					},
				}

				c := newTestClient(t, &cfg)
				return c, srv, last, g
			},
		},
		{
			name: "cookie session used when no access token",
			setup: func(t *testing.T) (*HTTPClient, *httptest.Server, *recordedRequest, golden) {
				g := golden{
					status:        200,
					body:          "ok",
					wantReqMethod: http.MethodGet,
					wantCookie:    "a=b;",
				}
				srv, last := makeServer(t, g)

				cfg := DefaultHTTPClientConfig()
				cfg.AuthProvider = fakeAuthProvider{
					authenticate: func(ctx context.Context) (dto.TokenInfo, error) {
						return dto.TokenInfo{
							Cookies: []*http.Cookie{
								{Name: "a", Value: "b"},
							},
							// expiry zero => treated valid
						}, nil
					},
				}

				c := newTestClient(t, &cfg)
				return c, srv, last, g
			},
		},
		{
			name: "captures set-cookie from response into token store",
			setup: func(t *testing.T) (*HTTPClient, *httptest.Server, *recordedRequest, golden) {
				g := golden{
					status:                 200,
					body:                   "ok",
					wantReqMethod:          http.MethodGet,
					expectStoredCookieName: "sid",
				}
				srv, last := makeServer(t, g)

				cfg := DefaultHTTPClientConfig()
				cfg.OAuthSource = &staticTokenSource{
					tok: &oauth2.Token{
						AccessToken: "abc",
						TokenType:   "bearer",
						Expiry:      time.Now().Add(1 * time.Hour),
					},
				}

				c := newTestClient(t, &cfg)
				return c, srv, last, g
			},
		},
		{
			name: "401 returns response and error",
			setup: func(t *testing.T) (*HTTPClient, *httptest.Server, *recordedRequest, golden) {
				g := golden{
					status:        http.StatusUnauthorized,
					body:          "nope",
					wantReqMethod: http.MethodGet,
					errContains:   "unauthorized",
				}
				srv, last := makeServer(t, g)

				cfg := DefaultHTTPClientConfig()
				c := newTestClient(t, &cfg)
				return c, srv, last, g
			},
		},
	}

	for _, cse := range cases {
		t.Run(cse.name, func(t *testing.T) {
			client, srv, last, g := cse.setup(t)
			defer srv.Close()

			reqCfg := DefaultHTTPRequestConfig()
			reqCfg.WithURL(srv.URL).WithMethod(g.wantReqMethod)

			// only set body when we expect it
			if g.wantBodyJSON != nil {
				// tests that want injected fields should start with orig=v
				if _, ok := g.wantBodyJSON["orig"]; ok {
					reqCfg.WithMethod(http.MethodPost)
					reqCfg.WithBody(map[string]any{"orig": "v"})
				}
			}
			// add headers when desired
			if g.wantHeaders != nil {
				reqCfg.WithHeaders(map[string]string{"X-FromSpec": "1"})
			}

			resp, err := client.ProcessRequest(context.Background(), &dto.RequestConfig{
				ReqConfig: &reqCfg,
			})

			if g.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), g.errContains) {
					t.Fatalf("err=%v; want contains %q", err, g.errContains)
				}
				// even on 401, response should be returned
				if resp.StatusCode != g.status {
					t.Fatalf("resp.StatusCode=%d; want %d", resp.StatusCode, g.status)
				}
				return
			}

			if err != nil {
				t.Fatalf("ProcessRequest error: %v", err)
			}

			// server-recorded request assertions
			if last.Method != g.wantReqMethod {
				t.Fatalf("method=%q; want %q", last.Method, g.wantReqMethod)
			}

			if g.wantAuth != "" {
				if got := last.Header.Get("Authorization"); got != g.wantAuth {
					t.Fatalf("Authorization=%q; want %q", got, g.wantAuth)
				}
			}

			if g.wantCookie != "" {
				got := last.Header.Get("Cookie")
				if !strings.Contains(got, g.wantCookie) {
					t.Fatalf("Cookie=%q; want contains %q", got, g.wantCookie)
				}
			}

			for k, v := range g.wantHeaders {
				if got := last.Header.Get(k); got != v {
					t.Fatalf("header %s=%q; want %q", k, got, v)
				}
			}

			if g.wantCT != "" {
				if last.ContentType != g.wantCT {
					t.Fatalf("Content-Type=%q; want %q", last.ContentType, g.wantCT)
				}
			}

			if g.wantBodyJSON != nil {
				var got map[string]any
				if err := json.Unmarshal(last.Body, &got); err != nil {
					t.Fatalf("unmarshal body=%q: %v", last.Body, err)
				}
				if !reflect.DeepEqual(got, g.wantBodyJSON) {
					t.Fatalf("json body=%v; want %v", got, g.wantBodyJSON)
				}
			}

			// cookie capture assertion
			if g.expectStoredCookieName != "" {
				client.tokenMu.RLock()
				defer client.tokenMu.RUnlock()
				found := false
				for _, ck := range client.token.Cookies {
					if ck != nil && ck.Name == g.expectStoredCookieName {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected stored cookie %q; got %v", g.expectStoredCookieName, client.token.Cookies)
				}
			}
		})
	}
}

func Test_HTTPClient_ensureToken_refreshBufferTriggersRefresh(t *testing.T) {
	ts := &staticTokenSource{
		tok: &oauth2.Token{
			AccessToken: "t1",
			TokenType:   "bearer",
			Expiry:      time.Now().Add(2 * time.Second),
		},
	}

	cfg := DefaultHTTPClientConfig()
	cfg.OAuthSource = ts
	cfg.RefreshBuffer = 30 * time.Second // larger than remaining lifetime -> refresh

	c := newTestClient(t, &cfg)

	// one request should cause token refresh (TokenSource.Token called)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	reqCfg := DefaultHTTPRequestConfig()
	reqCfg.WithURL(srv.URL).WithMethod(http.MethodGet)

	_, err := c.ProcessRequest(context.Background(), &dto.RequestConfig{ReqConfig: &reqCfg})
	if err != nil {
		t.Fatalf("ProcessRequest error: %v", err)
	}

	if ts.n.Load() != 1 {
		t.Fatalf("Token() calls=%d; want 1", ts.n.Load())
	}
}
