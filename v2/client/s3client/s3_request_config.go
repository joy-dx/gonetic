package s3client

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/joy-dx/gonetic/dto"
)

// S3RequestConfig defines the structure of an S3 request operation.
type S3RequestConfig struct {
	Operation string // "get", "put", "delete", "list"
	Bucket    string
	Key       string

	// Optional depending on operation
	Body        []byte
	Prefix      string
	ContentType string
	ExtraOpts   map[string]interface{}
	Headers     map[string]string
}

func (c *S3RequestConfig) Ref() dto.NetClientType {
	return NetClientS3Ref
}

type S3Request struct {
	Operation string
	Bucket    string
	Key       string

	Body        []byte
	Prefix      string
	ContentType string

	ExtraOpts map[string]any
	Headers   map[string]string

	// Deterministic prepared AWS inputs (built after middleware)
	PutInput    *s3.PutObjectInput
	GetInput    *s3.GetObjectInput
	DeleteInput *s3.DeleteObjectInput
	ListInput   *s3.ListObjectsV2Input
}

func (c *S3RequestConfig) NewRequest(ctx context.Context) (any, error) {
	r := &S3Request{
		Operation:   c.Operation,
		Bucket:      c.Bucket,
		Key:         c.Key,
		Body:        c.Body,
		Prefix:      c.Prefix,
		ContentType: c.ContentType,
		ExtraOpts:   make(map[string]any, len(c.ExtraOpts)),
		Headers:     make(map[string]string, len(c.Headers)),
	}

	for k, v := range c.Headers {
		r.Headers[k] = v
	}
	for k, v := range c.ExtraOpts {
		r.ExtraOpts[k] = v
	}

	return r, nil
}
