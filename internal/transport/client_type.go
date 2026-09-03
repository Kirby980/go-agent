package transport

import (
	"context"
	"net/http"
	"time"
)

// Client 把"连接池 + 重试退避 + 可选限流"封装成一个可复用的客户端。
// Do 是唯一出站入口：重试与限流统一在这里执行，调用方无法绕过。
type Client struct {
	http    *http.Client
	retry   RetryConfig
	limiter Limiter // 可空：nil 表示不限流
}
type RetryConfig struct {
	MaxRetries int           // 最多重试次数（不含首次）
	BaseDelay  time.Duration // 退避基数，如 500ms
	MaxDelay   time.Duration // 退避上限，如 10s
}

func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: 3,
		BaseDelay:  500 * time.Millisecond,
		MaxDelay:   10 * time.Second,
	}
}

// Limiter 抽象限流器。标准库没有内置限流，生产中通常用 golang.org/x/time/rate.Limiter
// （它的 Wait(ctx) error 正好满足这个接口）。接口定义在使用方，transport 无需 import rate。
type Limiter interface {
	Wait(ctx context.Context) error
}

type Option func(*Client)

func WithRetry(cfg RetryConfig) Option { return func(c *Client) { c.retry = cfg } }
func WithLimiter(l Limiter) Option     { return func(c *Client) { c.limiter = l } } // 如 rate.NewLimiter(10, 1)
func WithHTTPOptions(opts ...HTTPOption) Option {
	return func(c *Client) { c.http = NewHTTPClient(opts...) }
}

// NewClient 返回生产级 LLM HTTP 客户端；零参开箱即用，后面各章都用它。
func NewClient(opts ...Option) *Client {
	c := &Client{http: NewHTTPClient(), retry: DefaultRetryConfig()}
	for _, opt := range opts {
		opt(c)
	}
	return c
}
