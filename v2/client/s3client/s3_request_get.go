package s3client

import (
	"context"
	"fmt"
	"io"

	"github.com/joy-dx/gonetic/dto"
	"github.com/joy-dx/gonetic/utils"
)

func (c *S3Client) doGet(ctx context.Context, r *S3Request) (dto.Response, error) {
	out, err := c.client.GetObject(ctx, r.GetInput)
	if err != nil {
		return dto.Response{}, fmt.Errorf("s3 get object: %w", err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return dto.Response{}, fmt.Errorf("read s3 object: %w", err)
	}

	return dto.Response{
		StatusCode: 200,
		Body:       data,
		Headers:    utils.MapToHeader(out.Metadata),
	}, nil
}
