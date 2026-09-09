package dto

import (
	"context"
	"errors"
	"time"

	"github.com/joy-dx/gonetic/utils"
)

type ReqConfigInterface interface {
	Ref() NetClientType
	NewRequest(ctx context.Context) (any, error)
}

var ErrNilReqConfig = errors.New("nil ReqConfig provided")

type RequestConfig struct {
	// ClientRef Determines which http agent to use
	ClientRef string             `json:"client_ref" yaml:"client_ref"`
	ReqConfig ReqConfigInterface `json:"req_config" yaml:"req_config"`
	// ResponseObject Used for casting result to
	ResponseObject any              `json:"response_object" yaml:"response_object"`
	Timeout        time.Duration    `json:"timeout" yaml:"timeout"`
	MaxRetries     int              `json:"max_retries" yaml:"max_retries"`
	Delay          utils.RetryDelay `json:"-" yaml:"-"`
	TaskName       string           `json:"task_name" yaml:"task_name"`
}

func DefaultRequestConfig() RequestConfig {
	return RequestConfig{
		ClientRef:  NET_DEFAULT_CLIENT_REF,
		Timeout:    20 * time.Second,
		MaxRetries: 3,
		Delay:      utils.ExponentialBackoff{},
	}
}

func (c *RequestConfig) WithClientRef(ref string) *RequestConfig {
	c.ClientRef = ref
	return c
}

func (c *RequestConfig) WithReqConfig(cfg ReqConfigInterface) *RequestConfig {
	c.ReqConfig = cfg
	return c
}

func (c *RequestConfig) WithResponseObject(object interface{}) *RequestConfig {
	c.ResponseObject = object
	return c
}

func (c *RequestConfig) WithTimeout(duration time.Duration) *RequestConfig {
	c.Timeout = duration
	return c
}

func (c *RequestConfig) WithMaxRetries(count int) *RequestConfig {
	c.MaxRetries = count
	return c
}

func (c *RequestConfig) WithDelay(delay utils.RetryDelay) *RequestConfig {
	c.Delay = delay
	return c
}

func (c *RequestConfig) WithTaskName(name string) *RequestConfig {
	c.TaskName = name
	return c
}

func (c *RequestConfig) BuildRequest(ctx context.Context) (any, error) {
	if c.ReqConfig == nil {
		return nil, ErrNilReqConfig
	}
	return c.ReqConfig.NewRequest(ctx)
}
