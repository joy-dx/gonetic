package s3client

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/joy-dx/gonetic/dto"
)

const NetClientS3Ref dto.NetClientType = "net.client.s3"

type Middleware func(ctx context.Context, req *S3Request) error

// S3ClientConfig defines the static properties for an S3 client instance.
type S3ClientConfig struct {
	Region         string
	Credentials    aws.CredentialsProvider
	Middlewares    []Middleware
	ForcePathStyle bool
	Endpoint       string // optional custom endpoint
}

// Default config helpers
func DefaultS3ClientConfig(region string) S3ClientConfig {
	return S3ClientConfig{Region: region, Middlewares: []Middleware{}}
}

func (c *S3ClientConfig) WithMiddleware(m ...Middleware) *S3ClientConfig {
	c.Middlewares = append(c.Middlewares, m...)
	return c
}
