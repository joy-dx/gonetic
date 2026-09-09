package dto

import (
	"net/http"
	"time"
)

// TokenInfo represents active credential or session data.
// It supports both header-based tokens and cookie-based sessions.
type TokenInfo struct {
	// Authorization token, e.g. "Bearer abc123" or "Basic Zm9vOmJhcg=="
	AccessToken string
	// TokenType is inferred if not provided (default "Bearer").
	TokenType string
	// Expiry time. Optional — empty for cookie-only sessions.
	Expiry  time.Time
	Cookies []*http.Cookie
}

// IsExpired returns true if the token is close to or past expiry.
func (t *TokenInfo) IsExpired(buffer time.Duration) bool {
	if t.AccessToken == "" && len(t.Cookies) == 0 {
		return true
	}
	if t.Expiry.IsZero() {
		// Sessions with no expiry are considered indefinitely valid
		return false
	}
	return time.Now().After(t.Expiry.Add(-buffer))
}
