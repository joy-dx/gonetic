package gonetic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/joy-dx/gonetic/client/httpclient"
	"github.com/joy-dx/gonetic/dto"
	"github.com/joy-dx/gonetic/utils"
)

// Get RequestWithRetry
func (s *NetSvc) Get(ctx context.Context, url string, withRetry bool) (dto.Response, error) {
	httpRequestConfig := httpclient.DefaultHTTPRequestConfig()
	httpRequestConfig.WithURL(url)
	cfg := dto.DefaultRequestConfig()
	cfg.WithReqConfig(&httpRequestConfig).
		WithTaskName("GET " + url)

	if withRetry {
		return s.RequestWithRetry(ctx, &cfg)
	}
	return s.RequestOnce(ctx, &cfg)

}

// Post RequestWithRetry
func (s *NetSvc) Post(ctx context.Context, url string, payload map[string]interface{}, withRetry bool) (dto.Response, error) {
	httpRequestConfig := httpclient.DefaultHTTPRequestConfig()
	httpRequestConfig.WithURL(url).
		WithBody(payload).
		WithMethod(http.MethodPost)
	cfg := dto.DefaultRequestConfig()
	cfg.WithReqConfig(&httpRequestConfig).
		WithTaskName("POST " + url)

	if withRetry {
		return s.RequestWithRetry(ctx, &cfg)
	}
	return s.RequestOnce(ctx, &cfg)
}

func (s *NetSvc) RequestWithRetry(ctx context.Context, cfg *dto.RequestConfig) (dto.Response, error) {
	if cfg == nil {
		return dto.Response{}, errors.New("nil RequestConfig provided")
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.Delay == nil {
		cfg.Delay = utils.ConstantDelay{Period: 1}
	}
	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			cfg.Delay.Wait(cfg.TaskName, attempt)
		}

		resp, err := s.RequestOnce(ctx, cfg)
		if err != nil {
			lastErr = err
			// transient network errors → retry
			if utils.IsTemporaryErr(err) && attempt < cfg.MaxRetries {
				continue
			}
			return resp, err
		}

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("server error (%d)", resp.StatusCode)
			if attempt < cfg.MaxRetries {
				continue
			}
			// exhausted retries: return response + error
			return resp, fmt.Errorf(
				"failed after %d attempts: %w",
				cfg.MaxRetries+1,
				lastErr,
			)
		}
		return resp, nil
	}

	return dto.Response{}, fmt.Errorf("failed after %d attempts: %w", cfg.MaxRetries+1, lastErr)
}

func (s *NetSvc) RequestOnce(ctx context.Context, cfg *dto.RequestConfig) (dto.Response, error) {

	if cfg.ClientRef == "" {
		return dto.Response{}, errors.New("nil ClientRef provided")
	}

	if cfg.ReqConfig == nil {
		return dto.Response{}, errors.New("nil ReqConfig provided")
	}

	if cfg.TaskName == "" {
		cfg.TaskName = "http_request"
	}

	netClient, isOK := s.clients[cfg.ClientRef]
	if !isOK {
		return dto.Response{}, fmt.Errorf("client not found: %s", cfg.ClientRef)
	}

	// Sanity check that the req config matches the client type to avoid later casting confusion
	if netClient.Type() != cfg.ReqConfig.Ref() {
		return dto.Response{}, fmt.Errorf(
			"client type mismatch: client=%s(%s) req=%s",
			cfg.ClientRef,
			netClient.Type(),
			cfg.ReqConfig.Ref(),
		)
	}

	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	response, err := netClient.ProcessRequest(ctx, cfg)
	if err != nil {
		return dto.Response{}, fmt.Errorf("perform request: %w", err)
	}

	if cfg.ResponseObject != nil && len(response.Body) > 0 {
		if unmarshalErr := json.Unmarshal(response.Body, cfg.ResponseObject); unmarshalErr != nil {
			return response, fmt.Errorf("unmarshal response: %w", unmarshalErr)
		}
	}

	return response, nil
}
