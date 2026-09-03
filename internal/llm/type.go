package llm

import (
	"context"
	"encoding/json"
	"fmt"
)

// APIError 是各家 Provider 非 2xx 响应的统一形态。
// 各家错误体结构不同，但状态码 + 类型 + 文案这三样都有，够调用方判断该重试还是该改请求。
type APIError struct {
	Provider  string
	Status    int
	Type      string // 厂商的错误类型，如 invalid_request_error / rate_limit_error
	Message   string
	RequestID string // 报障给厂商时用
}

func (e *APIError) Error() string {
	s := fmt.Sprintf("%s: HTTP %d", e.Provider, e.Status)
	if e.Type != "" {
		s += " " + e.Type
	}
	if e.Message != "" {
		s += ": " + e.Message
	}
	if e.RequestID != "" {
		s += " (request_id=" + e.RequestID + ")"
	}
	return s
}

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

type ChatResponse struct {
	Content      string
	InputTokens  int
	OutputTokens int
}

type StreamChunk struct {
	Content string // 本次增量文本
	Err     error  // 出错时非空
}

type Provider interface {
	Name() string
	Capabilities() Capability // 声明当前统一接口真正支持的能力，路由时使用
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	// ChatStream 返回的 channel 会在流结束或出错时关闭；出错时最后一个 chunk 的 Err 非空。
	// 调用方必须读完 channel，或者 cancel ctx —— 提前 break 且不 cancel 会让实现内部的
	// goroutine 永久阻塞在发送上，连底层 HTTP 连接一起泄漏。
	ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)
}

// Capability 描述一个 Provider 的能力。
// M02 的统一请求只实现普通对话和流式输出，因此本章 Provider 只把 Streaming 设为 true。
// Thinking、Tools 字段预留给后续章节；统一请求结构尚未补齐前不能提前声明支持。
type Capability struct {
	Streaming bool
	Thinking  bool
	Tools     bool
}

func ParseInto[T any](raw string) (T, error) {
	var v T
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return v, err
	}
	return v, nil
}

func Ptr[T any](v T) *T { return &v }

func NewChatRequest(model string, messages []Message, opts ...Option) ChatRequest {
	req := ChatRequest{Model: model, Messages: messages}
	for _, opt := range opts {
		opt(&req)
	}
	return req
}

type Option func(*ChatRequest)

func WithTemperature(t float64) Option {
	return func(r *ChatRequest) { r.Temperature = &t }
}

func WithMaxTokens(n int) Option {
	return func(r *ChatRequest) { r.MaxTokens = n }
}

func WithStream(b bool) Option {
	return func(r *ChatRequest) { r.Stream = b }
}
