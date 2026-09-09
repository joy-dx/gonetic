package s3client

import (
	"bytes"
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/joy-dx/gonetic/dto"
)

func (c *S3Client) doList(ctx context.Context, r *S3Request) (dto.Response, error) {
	out, err := c.client.ListObjectsV2(ctx, r.ListInput)
	if err != nil {
		return dto.Response{}, fmt.Errorf("s3 list objects: %w", err)
	}

	buf := bytes.NewBuffer(nil)
	for _, obj := range out.Contents {
		fmt.Fprintf(buf, "%s\n", aws.ToString(obj.Key))
	}

	return dto.Response{
		StatusCode: 200,
		Body:       buf.Bytes(),
	}, nil
}
