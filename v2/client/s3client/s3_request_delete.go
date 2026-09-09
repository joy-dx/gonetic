package s3client

import (
	"context"
	"fmt"

	"github.com/joy-dx/gonetic/dto"
)

func (c *S3Client) doDelete(ctx context.Context, r *S3Request) (dto.Response, error) {
	_, err := c.client.DeleteObject(ctx, r.DeleteInput)
	if err != nil {
		return dto.Response{}, fmt.Errorf("s3 delete object: %w", err)
	}
	return dto.Response{StatusCode: 200}, nil
}
