package s3client

import (
	"context"
	"fmt"

	"github.com/joy-dx/gonetic/dto"
)

func (c *S3Client) ProcessRequest(ctx context.Context, reqCfg *dto.RequestConfig) (dto.Response, error) {
	cfg, ok := reqCfg.ReqConfig.(*S3RequestConfig)
	if !ok {
		return dto.Response{}, fmt.Errorf("problem casting to s3requestconfig")
	}

	reqAny, err := cfg.NewRequest(ctx)
	if err != nil {
		return dto.Response{}, fmt.Errorf("build request: %w", err)
	}
	r, ok := reqAny.(*S3Request)
	if !ok {
		return dto.Response{}, fmt.Errorf("problem casting built request to s3request")
	}

	for _, mw := range c.cfg.Middlewares {
		if err := mw(ctx, r); err != nil {
			return dto.Response{}, fmt.Errorf("middleware aborted: %w", err)
		}
	}

	if err := r.Finalize(); err != nil {
		return dto.Response{}, err
	}

	switch r.Operation {
	case "get":
		return c.doGet(ctx, r)
	case "put":
		return c.doPut(ctx, r)
	case "delete":
		return c.doDelete(ctx, r)
	case "list":
		return c.doList(ctx, r)
	default:
		return dto.Response{}, fmt.Errorf("unsupported s3 operation: %s", r.Operation)
	}
}
