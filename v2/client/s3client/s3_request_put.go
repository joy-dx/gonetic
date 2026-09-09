package s3client

import (
	"context"
	"fmt"

	"github.com/joy-dx/gonetic/dto"
)

func (c *S3Client) doPut(ctx context.Context, r *S3Request) (dto.Response, error) {
	_, err := c.client.PutObject(ctx, r.PutInput)
	if err != nil {
		return dto.Response{}, fmt.Errorf("s3 put object: %w", err)
	}
	return dto.Response{StatusCode: 200}, nil
}
