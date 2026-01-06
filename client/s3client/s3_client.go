package s3client

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/joy-dx/gonetic/dto"
)

// s3API This internal interface abstracts the s3 client for easier testing
type s3API interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

type S3Client struct {
	NetClient dto.NetClient
	cfg       *S3ClientConfig
	client    s3API
	mu        sync.RWMutex
}

func NewS3Client(ref string, cfg *S3ClientConfig) (*S3Client, error) {
	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(cfg.Credentials),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.ForcePathStyle
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
	})

	return &S3Client{
		cfg:    cfg,
		client: client,
		NetClient: dto.NetClient{
			Name:        "S3 Client",
			Ref:         ref,
			ClientType:  NetClientS3Ref,
			Description: "Performs basic S3 operations (get, put, list, delete)",
		},
	}, nil
}

func (c *S3Client) Ref() string {
	return c.NetClient.Ref
}

func (c *S3Client) Type() dto.NetClientType {
	return NetClientS3Ref
}
