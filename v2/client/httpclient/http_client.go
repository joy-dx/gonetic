package httpclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/joy-dx/gonetic/v2/config"
	"github.com/joy-dx/gonetic/v2/dto"
)

// -----------------------------------------------------------------------------
// PERSISTENT CLIENT IMPLEMENTATION
// -----------------------------------------------------------------------------

// HTTPClient is a high-level wrapper around dto.NetInterface,
// providing automatic authentication and session management.
//
// It supports multiple authentication modes:
//   - OAuth2 TokenSource (golang.org/x/oauth2)
//   - Custom AuthProvider
//   - Cookie-based sessions
//
// HTTPClient is suitable for long-lived service integrations where
// multiple requests share authentication state safely.

const NetClientHTTPRef dto.NetClientType = "net.client.http"

type HTTPClient struct {
	NetClient dto.NetClient `json:"net_client" yaml:"net_client"`
	cfg       *HTTPClientConfig
	netCfg    *config.NetSvcConfig
	client    *http.Client
	token     dto.TokenInfo
	tokenMu   sync.RWMutex
}

func NewHTTPClient(ref string, netCfg *config.NetSvcConfig, cfg *HTTPClientConfig) *HTTPClient {
	return &HTTPClient{
		cfg:    cfg,
		netCfg: netCfg,
		NetClient: dto.NetClient{
			Name:        "HTTP Client",
			Ref:         ref,
			ClientType:  NetClientHTTPRef,
			Description: "Perform HTTP requests to given URLs including auth support",
		},
		client: &http.Client{
			Timeout: netCfg.RequestTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        50,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
				DisableKeepAlives:   false,
				Proxy:               http.ProxyFromEnvironment,
			},
		},
	}
}

func (c *HTTPClient) Ref() string {
	return c.NetClient.Ref
}
func (c *HTTPClient) Type() dto.NetClientType {
	return NetClientHTTPRef
}

// -----------------------------------------------------------------------------
// REQUEST EXECUTION
// -----------------------------------------------------------------------------
// RequestWithRetry executes one authenticated, middleware-wrapped call through the underlying dto.NetInterface.
// Automatically handles token Lifetimes, OAuth2 renewal, and cookie sessions.
//
// If multiple authentication mechanisms are configured, OAuth2 takes precedence.
// AuthProvider is used as a fallback.
func (c *HTTPClient) ProcessRequest(ctx context.Context, inCfg *dto.RequestConfig) (dto.Response, error) {
	cfg, castOk := inCfg.ReqConfig.(*HTTPRequestConfig)
	if !castOk {
		return dto.Response{}, errors.New("problem casting to httprequestconfig")
	}

	reqAny, err := cfg.NewRequest(ctx)
	if err != nil {
		return dto.Response{}, fmt.Errorf("build request: %w", err)
	}
	reqCfg, ok := reqAny.(*HTTPRequest)
	if !ok {
		return dto.Response{}, errors.New("problem casting built request to httprequest")
	}

	for _, mw := range c.cfg.Middlewares {
		if err := mw(ctx, reqCfg); err != nil {
			return dto.Response{}, fmt.Errorf("middleware aborted: %w", err)
		}
	}

	if err := c.ensureToken(ctx); err != nil {
		return dto.Response{}, fmt.Errorf("ensure token: %w", err)
	}

	// Step 3: attach credentials (Authorization or Cookies)
	c.tokenMu.RLock()
	c.attachAuth(reqCfg)
	c.tokenMu.RUnlock()

	if err := reqCfg.FinalizeBody(); err != nil {
		return dto.Response{}, err
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		reqCfg.Method,
		reqCfg.URL,
		bytes.NewReader(reqCfg.BodyBytes),
	)
	if err != nil {
		return dto.Response{}, fmt.Errorf("create request: %w", err)
	}

	for k, v := range reqCfg.Headers {
		if k == "Authorization" && httpReq.Header.Get("Authorization") != "" {
			continue
		}
		httpReq.Header.Set(k, v)
	}

	if reqCfg.ContentType != "" && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", reqCfg.ContentType)
	}

	// Defensive client.Do handling — httpResp may be non-nil with error
	httpResp, reqErr := c.client.Do(httpReq)
	if httpResp != nil {
		defer func() {
			io.Copy(io.Discard, httpResp.Body) // drain fully for connection reuse
			httpResp.Body.Close()
		}()
	}
	if reqErr != nil {
		return dto.Response{}, fmt.Errorf("perform request: %w", reqErr)
	}

	bodyBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return dto.Response{}, fmt.Errorf("read body: %w", err)
	}

	response := dto.Response{
		StatusCode: httpResp.StatusCode,
		Headers:    httpResp.Header.Clone(),
		Body:       bodyBytes,
	}

	// Capture cookies, prunes if expired
	if setCookies := response.Headers["Set-Cookie"]; len(setCookies) > 0 {
		c.captureCookiesFromResponse(response)
	}

	// Guard unauthorized error type explicitly
	if response.StatusCode == http.StatusUnauthorized {
		return response, fmt.Errorf("unauthorized: %s", cfg.URL)
	}

	return response, nil
}

func (c *HTTPClient) Do(
	req *http.Request,
) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header = req.Header.Clone()

	headers := make(map[string]string, len(req.Header))
	for name := range req.Header {
		headers[name] = req.Header.Get(name)
	}

	prepared := &HTTPRequest{
		Method:  req.Method,
		URL:     req.URL.String(),
		Headers: headers,
	}

	for _, middleware := range c.cfg.Middlewares {
		if err := middleware(req.Context(), prepared); err != nil {
			return nil, fmt.Errorf("download middleware: %w", err)
		}
	}

	if err := c.ensureToken(req.Context()); err != nil {
		return nil, fmt.Errorf("ensure download token: %w", err)
	}

	c.tokenMu.RLock()
	c.attachAuth(prepared)
	c.tokenMu.RUnlock()

	// Preserve repeated header values unless middleware/auth changed them.
	for name, value := range prepared.Headers {
		if value != req.Header.Get(name) {
			req.Header.Set(name, value)
		}
	}

	client := DownloadHTTPClient(c.client)
	resp, err := client.Do(req)

	if resp != nil {
		c.captureCookiesFromResponse(dto.Response{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header.Clone(),
		})
	}

	return resp, err
}

func DownloadHTTPClient(base *http.Client) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}

	client := *base
	previousPolicy := base.CheckRedirect

	client.CheckRedirect = func(
		req *http.Request,
		via []*http.Request,
	) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}

		if previousPolicy != nil {
			if err := previousPolicy(req, via); err != nil {
				return err
			}
		}

		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return fmt.Errorf(
				"unsupported redirect scheme %q",
				req.URL.Scheme,
			)
		}

		// Do not silently downgrade authenticated HTTPS downloads.
		for _, previous := range via {
			if previous.URL.Scheme == "https" &&
				req.URL.Scheme == "http" {
				return errors.New("refusing HTTPS-to-HTTP redirect")
			}
		}

		// Custom headers may contain credentials too. Once a redirect
		// crosses origins, retain only download-safe headers.
		crossedOrigin := false
		for _, previous := range via {
			if !sameDownloadOrigin(previous.URL, req.URL) {
				crossedOrigin = true
				break
			}
		}

		if crossedOrigin {
			for name := range req.Header {
				switch strings.ToLower(name) {
				case "accept", "accept-encoding", "user-agent",
					"range", "if-range":
				default:
					req.Header.Del(name)
				}
			}
		}

		return nil
	}

	return &client
}

func sameDownloadOrigin(a, b *url.URL) bool {
	port := func(u *url.URL) string {
		if value := u.Port(); value != "" {
			return value
		}
		if u.Scheme == "https" {
			return "443"
		}
		return "80"
	}

	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) &&
		port(a) == port(b)
}
