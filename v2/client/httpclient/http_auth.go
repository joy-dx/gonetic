package httpclient

import (
	"fmt"
	"strings"
)

// normalizeAuthType ensures proper "Bearer", "Basic", or custom capitalization.
func normalizeAuthType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "bearer":
		return "Bearer"
	case "basic":
		return "Basic"
	default:
		if t == "" {
			return "Bearer"
		}
		return t
	}
}

// -----------------------------------------------------------------------------
// HEADER + COOKIE MANAGEMENT
// -----------------------------------------------------------------------------

// attachAuth injects auth credentials or cookies into a dto.RequestConfig.
func (c *HTTPClient) attachAuth(cfg *HTTPRequest) {
	if cfg.Headers == nil {
		cfg.Headers = map[string]string{}
	}

	if c.token.AccessToken != "" {
		authHeader := fmt.Sprintf("%s %s", normalizeAuthType(c.token.TokenType), c.token.AccessToken)
		cfg.Headers["Authorization"] = authHeader
		return
	}

	if len(c.token.Cookies) > 0 {
		merged := ""
		for _, ck := range c.token.Cookies {
			merged += ck.Name + "=" + ck.Value + "; "
		}
		cfg.Headers["Cookie"] = merged
	}
}
