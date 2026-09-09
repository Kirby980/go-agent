// Package openai 提供了对 OpenAI 及兼容协议（如 DeepSeek、豆包、Ollama 等）的模型接口适配。
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Kirby980/agent/internal/transport"
	"github.com/Kirby980/agent/llm"
)

// Provider 实现 llm.Provider 接口，负责与 OpenAI 兼容 API 进行通信。
type Provider struct {
	name    string
	baseURL string
	apiKey  string
	client  *transport.Client
}

// Config 包含连接 OpenAI 兼容服务所需的配置参数。
type Config struct {
	Name    string // 提供商名称标识
	BaseURL string // API 基地址（如 "https://api.openai.com/v1"）
	APIKey  string // 认证 API 密钥
}

type chatReq struct {
	Model       string       `json:"model"`
	Messages    []chatMsg    `json:"messages"`
	Temperature *float64     `json:"temperature,omitempty"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	Stream      bool         `json:"stream,omitempty"`
	Stop        []string     `json:"stop,omitempty"` // 命中任一序列时模型停止生成
	Tools       []openaiTool `json:"tools,omitempty"`
}

// New 创建并返回一个 OpenAI 兼容的 Provider 实例。
func New(cfg Config) *Provider {
	return &Provider{
		name:    cfg.Name,
		baseURL: cfg.BaseURL,
		apiKey:  cfg.APIKey,
		client:  transport.NewClient(),
	}
}

func (p *Provider) Name() string { return p.name }

func (p *Provider) Capabilities() llm.Capability {
	return llm.Capability{Streaming: true, Tools: true}
}

// parseAPIError 解析 OpenAI 兼容的错误响应：
//
//	{"error":{"message":"...","type":"invalid_request_error","code":"..."}}
//
// 解析不出这个结构就把原始 body 原样带上——各家兼容接口的错误体并不总是标准的。
func (p *Provider) parseAPIError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10)) // ponytail: 错误体不会大，8KB 封顶防内存打爆
	e := &llm.APIError{
		Provider:  p.name,
		Status:    resp.StatusCode,
		RequestID: resp.Header.Get("x-request-id"),
	}
	var wire struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &wire) == nil && wire.Error.Message != "" {
		e.Type, e.Message = wire.Error.Type, wire.Error.Message
		if e.Type == "" {
			e.Type = wire.Error.Code
		}
		return e
	}
	e.Message = strings.TrimSpace(string(raw))
	return e
}

func (p *Provider) validateMessages(messages []llm.Message) error {
	for _, m := range messages {
		if m.Role == llm.RoleTool {
			return nil
		}
	}
	return nil
}

// Chat openai非流式接口
/* 返回数据示例
{
  "id": "chatcmpl-123abc",
  "object": "chat.completion",
  "created": 1710000000,
  "model": "gpt-4o",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "你好！我是 AI 助手，有什么可以帮你？"
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 15,
    "completion_tokens": 12,
    "total_tokens": 27
  }
}
*/
func (p *Provider) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	if err := p.validateMessages(req.Messages); err != nil {
		return nil, err
	}
	req.Stream = false // Chat 是非流式入口；流式走 ChatStream，别让调用方误传出一个解析不了的 SSE body
	payload := chatReq{
		Model:       req.Model,
		Messages:    toOpenAIMessages(req.Messages),
		Tools:       toOpenAITools(req.Tools),
		Stream:      req.Stream,
		Stop:        req.Stop,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%s: 序列化请求: %w", p.name, err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		p.baseURL+"/chat/completions",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, p.parseAPIError(resp)
	}

	var out struct {
		Choices []struct {
			Message struct {
				Role      string         `json:"role"`
				Content   string         `json:"content"`
				ToolCalls []respToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("%s: 空响应", p.name)
	}
	msg := out.Choices[0].Message
	return &llm.ChatResponse{
		Content:      msg.Content,
		InputTokens:  out.Usage.PromptTokens,
		OutputTokens: out.Usage.CompletionTokens,
		ToolCalls:    parseToolCalls(msg.ToolCalls),
	}, nil
}

// errStreamDone 是 [DONE] 的哨兵，用来让 ParseSSE 立刻收尾。
// 不早退的话：部分兼容网关在 [DONE] 之后仍保持连接发心跳，Scan 会一直阻塞，
// 调用方的 range 永远不结束。
var errStreamDone = errors.New("stream done")

// ChatStream openai流式接口
/* 返回数据示例
OpenAI:
data: {"choices":[{"delta":{"content":"H"}}]}
data: {"choices":[{"delta":{"content":"i"}}]}
data: [DONE]
*/
//
// 调用方必须把返回的 channel 读完，或者 cancel ctx。提前 break 且不 cancel 会让
// 内部 goroutine 永久阻塞在发送上，连底层连接一起泄漏。
func (p *Provider) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	err := p.validateMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	payload := chatReq{
		Model:       req.Model,
		Messages:    toOpenAIMessages(req.Messages),
		Tools:       toOpenAITools(req.Tools),
		Stream:      true,
		Stop:        req.Stop,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%s: 序列化请求: %w", p.name, err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		p.baseURL+"/chat/completions",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream") // 少数网关据此才走流式
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, p.parseAPIError(resp)
	}
	out := make(chan llm.StreamChunk)
	go func() {
		defer close(out)
		defer resp.Body.Close()

		type partialToolCall struct {
			id   string
			name string
			args strings.Builder
		}
		toolCallsMap := make(map[int]*partialToolCall)
		maxIndex := -1

		streamErr := transport.ParseSSE(resp.Body, func(data []byte) error {
			ev, done, err := p.parseOpenAIStreamEvent(data)
			if err != nil {
				return err
			}
			if done {
				return errStreamDone // 提前收尾，别等服务端关连接
			}
			for _, tc := range ev.ToolCalls {
				ptc, ok := toolCallsMap[tc.Index]
				if !ok {
					ptc = &partialToolCall{}
					toolCallsMap[tc.Index] = ptc
					if tc.Index > maxIndex {
						maxIndex = tc.Index
					}
				}
				if tc.ID != "" {
					ptc.id = tc.ID
				}
				if tc.Function.Name != "" {
					ptc.name = tc.Function.Name
				}
				ptc.args.WriteString(tc.Function.Arguments)
			}
			if ev.Content == "" {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case out <- llm.StreamChunk{Content: ev.Content}:
				return nil
			}
		})
		switch {
		case errors.Is(streamErr, errStreamDone):
			streamErr = nil // 正常收尾
		case streamErr == nil:
			streamErr = fmt.Errorf("%s: 流在 [DONE] 前结束", p.name)
		}
		if streamErr != nil {
			select {
			case <-ctx.Done():
			case out <- llm.StreamChunk{Err: streamErr}:
			}
			return
		}

		if len(toolCallsMap) > 0 {
			var calls []llm.ToolCall
			for i := 0; i <= maxIndex; i++ {
				if ptc, ok := toolCallsMap[i]; ok {
					calls = append(calls, llm.ToolCall{
						ID:   ptc.id,
						Name: ptc.name,
						Args: json.RawMessage(ptc.args.String()),
					})
				}
			}
			select {
			case <-ctx.Done():
			case out <- llm.StreamChunk{ToolCalls: calls}:
			}
		}
	}()
	return out, nil
}

type streamToolCallChunk struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type openAIStreamEvent struct {
	Content   string
	ToolCalls []streamToolCallChunk
}

func (p *Provider) parseOpenAIStreamEvent(data []byte) (event openAIStreamEvent, done bool, err error) {
	if string(data) == "[DONE]" {
		return openAIStreamEvent{}, true, nil
	}
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content   string                `json:"content"`
				ToolCalls []streamToolCallChunk `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &chunk); err != nil {
		return openAIStreamEvent{}, false, fmt.Errorf("%s: 解析流事件失败: %w (原文 %.200q)", p.name, err, data)
	}
	if chunk.Error != nil {
		kind := chunk.Error.Type
		if kind == "" {
			kind = chunk.Error.Code
		}
		return openAIStreamEvent{}, false, fmt.Errorf("%s: 流内错误 %s: %s", p.name, kind, chunk.Error.Message)
	}
	if len(chunk.Choices) == 0 {
		return openAIStreamEvent{}, false, nil
	}
	return openAIStreamEvent{
		Content:   chunk.Choices[0].Delta.Content,
		ToolCalls: chunk.Choices[0].Delta.ToolCalls,
	}, false, nil
}

// parseOpenAIDelta 按照openai流格式解析数据
func (p *Provider) parseOpenAIDelta(data []byte) (delta string, done bool, err error) {
	ev, done, err := p.parseOpenAIStreamEvent(data)
	if err != nil || done {
		return "", done, err
	}
	return ev.Content, false, nil
}
