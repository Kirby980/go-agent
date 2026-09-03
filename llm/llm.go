// Package llm 提供了大语言模型的统一调用契约（Provider 接口）、消息模型（Message/Role）
// 以及请求/响应与流式分块定义。
package llm

import (
	"context"
	"encoding/json"
	"fmt"
)

// ToolDef 是给模型看的工具元数据定义（包含名称、描述与 JSON Schema 参数规范）。
type ToolDef struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ToolCall 是模型返回的结构化工具调用意图。
type ToolCall struct {
	ID   string          `json:"id"`   // 厂商返回的调用 ID，回传工具执行结果时需带上以做配对
	Name string          `json:"name"` // 目标工具名称
	Args json.RawMessage `json:"args"` // 模型生成的工具参数 JSON
}

// APIError 统一封装各个模型厂商非 2xx HTTP 状态码的错误响应。
type APIError struct {
	Provider  string
	Status    int
	Type      string // 厂商错误类型，如 invalid_request_error / rate_limit_error
	Message   string
	RequestID string // 平台请求追踪 ID
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

// Role 表示对话消息发送方的角色身份。
type Role string

const (
	// RoleSystem 系统级指令提示词角色。
	RoleSystem Role = "system"
	// RoleUser 终端用户角色。
	RoleUser Role = "user"
	// RoleAssistant 大模型助手角色。
	RoleAssistant Role = "assistant"
	// RoleTool 外部工具执行结果回填角色。
	RoleTool Role = "tool"
)

// Message 表示对话历史中的单条消息实体。
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // Assistant 角色消息：发起的工具调用列表
	ToolCallID string     `json:"tool_call_id,omitempty"` // Tool 角色消息：本条观察结果关联的 ToolCall.ID
}

// ChatRequest 表示统一的模型聊天请求入参。
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
	Stop        []string  `json:"stop,omitempty"` // 命中任一序列时模型停止生成
	Tools       []ToolDef
}

// ChatResponse 表示单次非流式聊天调用的返回结果。
type ChatResponse struct {
	Content      string
	InputTokens  int
	OutputTokens int
	ToolCalls    []ToolCall
}

// StreamChunk 表示流式输出过程中的单个文本增量块。
type StreamChunk struct {
	Content string // 本次增量文本
	Err     error  // 出错时非空
}

// Provider 是各类大模型供应商（OpenAI, Claude 等）必须实现的统一适配接口。
type Provider interface {
	// Name 返回供应商标识名称（如 "openai", "claude"）
	Name() string
	// Capabilities 声明当前适配器支持的特性能力（如流式、Tools 原生支持等）
	Capabilities() Capability
	// Chat 发起阻塞式单次聊天请求
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	// ChatStream 发起流式聊天请求，返回只读增量 channel
	ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)
}

// Capability 描述一个 Provider 所支持的特性集。
type Capability struct {
	Streaming bool
	Thinking  bool
	Tools     bool
}

// ParseInto 将 JSON 字符串反序列化解析到指定泛型结构体中。
func ParseInto[T any](raw string) (T, error) {
	var v T
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return v, err
	}
	return v, nil
}

// Ptr 返回任意值的指针辅助函数。
func Ptr[T any](v T) *T { return &v }

// NewChatRequest 构造带有可选参数的统一聊天请求对象。
func NewChatRequest(model string, messages []Message, opts ...Option) ChatRequest {
	req := ChatRequest{Model: model, Messages: messages}
	for _, opt := range opts {
		opt(&req)
	}
	return req
}

// Option 定义 ChatRequest 的函数式配置选项。
type Option func(*ChatRequest)

// WithTemperature 设置请求采样温度。
func WithTemperature(t float64) Option {
	return func(r *ChatRequest) { r.Temperature = &t }
}

// WithMaxTokens 设置最大生成 token 数量。
func WithMaxTokens(n int) Option {
	return func(r *ChatRequest) { r.MaxTokens = n }
}

// WithStream 设置是否启用流式传输。
func WithStream(b bool) Option {
	return func(r *ChatRequest) { r.Stream = b }
}
