package dto

import (
	"net/http"
	"testing"
	"time"
)

func TestTokenInfo_IsExpired_Golden(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		name   string
		tok    TokenInfo
		buffer time.Duration
		want   bool
	}{
		{
			name:   "empty token expired",
			tok:    TokenInfo{},
			buffer: 30 * time.Second,
			want:   true,
		},
		{
			name: "cookie only no expiry not expired",
			tok: TokenInfo{
				Cookies: []*http.Cookie{{Name: "sid", Value: "1"}},
			},
			buffer: 30 * time.Second,
			want:   false,
		},
		{
			name: "access token no expiry not expired",
			tok: TokenInfo{
				AccessToken: "abc",
				TokenType:   "Bearer",
			},
			buffer: 30 * time.Second,
			want:   false,
		},
		{
			name: "expired by time",
			tok: TokenInfo{
				AccessToken: "abc",
				Expiry:      now.Add(-1 * time.Minute),
			},
			buffer: 30 * time.Second,
			want:   true,
		},
		{
			name: "expires soon within buffer is treated expired",
			tok: TokenInfo{
				AccessToken: "abc",
				Expiry:      now.Add(10 * time.Second),
			},
			buffer: 30 * time.Second,
			want:   true,
		},
		{
			name: "valid beyond buffer",
			tok: TokenInfo{
				AccessToken: "abc",
				Expiry:      now.Add(2 * time.Minute),
			},
			buffer: 30 * time.Second,
			want:   false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.tok.IsExpired(tt.buffer); got != tt.want {
				t.Fatalf("got=%v want %v", got, tt.want)
			}
		})
	}
}
