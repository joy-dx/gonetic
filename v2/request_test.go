package gonetic

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/joy-dx/gonetic/dto"
)

// fakeReqConfig satisfies dto.ReqConfigInterface for tests.
// It must match the fake client's Type() to pass the type mismatch check.
type fakeReqConfig struct {
	typ dto.NetClientType
}

func (f fakeReqConfig) Ref() dto.NetClientType { return f.typ }

func (f fakeReqConfig) NewRequest(ctx context.Context) (any, error) {
	return struct{}{}, nil
}

func TestNetSvc_RequestOnce_Golden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cfg      *dto.RequestConfig
		client   dto.NetClientInterface
		wantErr  bool
		wantCode int
		wantBody string
	}{
		{
			name:    "nil client ref errors",
			cfg:     &dto.RequestConfig{ClientRef: ""},
			wantErr: true,
		},
		{
			name:    "client not found errors",
			cfg:     &dto.RequestConfig{ClientRef: "missing"},
			wantErr: true,
		},
		{
			name: "wraps client error",
			cfg:  &dto.RequestConfig{ClientRef: "c"},
			client: &fakeNetClient{ref: "c", fn: func(ctx context.Context, cfg *dto.RequestConfig) (dto.Response, error) {
				return dto.Response{}, errors.New("boom")
			}},
			wantErr: true,
		},
		{
			name: "successful",
			cfg:  &dto.RequestConfig{ClientRef: "c"},
			client: &fakeNetClient{ref: "c", fn: func(ctx context.Context, cfg *dto.RequestConfig) (dto.Response, error) {
				return dto.Response{StatusCode: 201, Body: []byte("ok")}, nil
			}},
			wantCode: 201,
			wantBody: "ok",
		},
		{
			name: "timeout cancels context",
			cfg:  &dto.RequestConfig{ClientRef: "c", Timeout: 10 * time.Millisecond},
			client: &fakeNetClient{ref: "c", fn: func(ctx context.Context, cfg *dto.RequestConfig) (dto.Response, error) {
				<-ctx.Done()
				return dto.Response{}, ctx.Err()
			}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// ensure ReqConfig is set so RequestOnce doesn't fail early.
			if tt.cfg != nil && tt.cfg.ReqConfig == nil && tt.cfg.ClientRef != "" {
				tt.cfg.ReqConfig = fakeReqConfig{typ: ""}
			}

			s := newTestSvc(t)
			if tt.client != nil && tt.cfg != nil && tt.cfg.ClientRef != "" {
				s.RegisterClient(tt.cfg.ClientRef, tt.client)
			}

			resp, err := s.RequestOnce(context.Background(), tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if resp.StatusCode != tt.wantCode {
					t.Fatalf("code=%d want %d", resp.StatusCode, tt.wantCode)
				}
				if string(resp.Body) != tt.wantBody {
					t.Fatalf("body=%q want %q", string(resp.Body), tt.wantBody)
				}
			}
		})
	}
}

func TestNetSvc_RequestWithRetry_Golden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		max  int
		seq  []struct {
			resp dto.Response
			err  error
		}
		wantCalls int
		wantErr   bool
		wantCode  int
	}{
		{
			name: "retries temporary error then succeeds",
			max:  3,
			seq: []struct {
				resp dto.Response
				err  error
			}{
				{err: tempErr{msg: "temp"}},
				{resp: dto.Response{StatusCode: 200}},
			},
			wantCalls: 2,
			wantCode:  200,
		},
		{
			name: "does not retry non-temporary error (but your IsTemporaryErr defaults true for most errors!)",
			max:  1,
			seq: []struct {
				resp dto.Response
				err  error
			}{
				{err: fmt.Errorf("generic")},
				{resp: dto.Response{StatusCode: 200}},
			},
			// With current IsTemporaryErr, generic errors are considered temporary -> will retry.
			wantCalls: 2,
			wantCode:  200,
		},
		{
			name: "retries on 5xx then succeeds",
			max:  2,
			seq: []struct {
				resp dto.Response
				err  error
			}{
				{resp: dto.Response{StatusCode: 503}},
				{resp: dto.Response{StatusCode: 200}},
			},
			wantCalls: 2,
			wantCode:  200,
		},
		{
			name: "stops after max retries (5xx returns error now)",
			max:  1,
			seq: []struct {
				resp dto.Response
				err  error
			}{
				{resp: dto.Response{StatusCode: 503}},
				{resp: dto.Response{StatusCode: 503}},
			},
			wantCalls: 2,
			wantErr:   true,
			wantCode:  503, // still assert response is returned
		},
		{
			name:    "nil cfg errors",
			seq:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newTestSvc(t)

			if tt.name == "nil cfg errors" {
				_, err := s.RequestWithRetry(context.Background(), nil)
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}

			i := 0
			client := &fakeNetClient{
				ref: "c",
				fn: func(ctx context.Context, cfg *dto.RequestConfig) (dto.Response, error) {
					if i >= len(tt.seq) {
						return dto.Response{}, errors.New("sequence exhausted")
					}
					out := tt.seq[i]
					i++
					return out.resp, out.err
				},
			}
			s.RegisterClient("c", client)

			cfg := dto.DefaultRequestConfig()
			cfg.ClientRef = "c"
			cfg.MaxRetries = tt.max
			cfg.Delay = noWaitDelay{}
			cfg.ReqConfig = fakeReqConfig{typ: ""}

			resp, err := s.RequestWithRetry(context.Background(), &cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
			if resp.StatusCode != tt.wantCode {
				t.Fatalf("code=%d want %d", resp.StatusCode, tt.wantCode)
			}

			client.mu.Lock()
			calls := client.call
			client.mu.Unlock()
			if calls != tt.wantCalls {
				t.Fatalf("calls=%d want %d", calls, tt.wantCalls)
			}

			if !tt.wantErr && resp.StatusCode != tt.wantCode {
				t.Fatalf("code=%d want %d", resp.StatusCode, tt.wantCode)
			}
		})
	}
}
