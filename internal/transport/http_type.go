package transport

import (
	"net"
	"net/http"
	"time"
)

// HTTPConfig 收拢 HTTP 客户端的可调项。
type HTTPConfig struct {
	DialTimeout         time.Duration // 建连超时
	KeepAlive           time.Duration
	MaxIdleConns        int
	MaxIdleConnsPerHost int // 默认只有 2，调模型时远远不够
	IdleConnTimeout     time.Duration
	TLSHandshakeTimeout time.Duration
}

// DefaultHTTPConfig 是一组适合调 LLM API 的稳妥默认值。
func DefaultHTTPConfig() HTTPConfig {
	return HTTPConfig{
		DialTimeout:         5 * time.Second,
		KeepAlive:           30 * time.Second,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
	}
}

// HTTPOption 覆盖默认配置。
type HTTPOption func(*HTTPConfig)

func WithDialTimeout(d time.Duration) HTTPOption { return func(c *HTTPConfig) { c.DialTimeout = d } }
func WithMaxIdleConnsPerHost(n int) HTTPOption {
	return func(c *HTTPConfig) { c.MaxIdleConnsPerHost = n }
}

// 其余字段按同一模式按需添加 WithXxx。
// newTransport 把配置组装成 *http.Transport —— 与 client 构造分离，单一职责。
func newTransport(c HTTPConfig) *http.Transport {
	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   c.DialTimeout,
			KeepAlive: c.KeepAlive,
		}).DialContext,
		MaxIdleConns:          c.MaxIdleConns,
		MaxIdleConnsPerHost:   c.MaxIdleConnsPerHost,
		IdleConnTimeout:       c.IdleConnTimeout,
		TLSHandshakeTimeout:   c.TLSHandshakeTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// NewHTTPClient 返回一个适合调 LLM API 的客户端；不传参用默认值，也可用 Option 覆盖。
func NewHTTPClient(opts ...HTTPOption) *http.Client {
	cfg := DefaultHTTPConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	// 注意：不设 client.Timeout —— 流式(SSE)是长连接，整体超时会把它掐断；
	// 超时交给 context 逐次控制，这里只兜底拨号/握手等阶段。
	return &http.Client{Transport: newTransport(cfg)}
}
