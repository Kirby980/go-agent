// Package claude 提供了对 Anthropic Claude 官方 Messages API 的模型接口适配。
package claude

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

// Provider 实现 llm.Provider 接口，负责与 Anthropic Claude Messages API 通信。
type Provider struct {
	name    string
	baseURL string
	apiKey  string
	client  *transport.Client
}

// Config 包含连接 Anthropic Claude 服务所需的配置。
type Config struct {
	Name    string // 提供商名称标识
	BaseURL string // API 基地址（如 "https://api.anthropic.com"）
	APIKey  string // Anthropic API Key
}

// New 创建并返回一个 Claude Provider 实例。
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

// parseAPIError 解析 Anthropic 的错误响应：
//
//	{"type":"error","error":{"type":"invalid_request_error","message":"..."},"request_id":"req_011C..."}
//
// request_id 报障给 Anthropic 时要用，优先取 body 里的，没有就取响应头。
func (p *Provider) parseAPIError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10)) // ponytail: 错误体不会大，8KB 封顶防内存打爆
	e := &llm.APIError{
		Provider:  p.name,
		Status:    resp.StatusCode,
		RequestID: resp.Header.Get("request-id"),
	}
	var wire struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	if json.Unmarshal(raw, &wire) == nil && wire.Error.Message != "" {
		e.Type, e.Message = wire.Error.Type, wire.Error.Message
		if wire.RequestID != "" {
			e.RequestID = wire.RequestID
		}
		return e
	}
	e.Message = strings.TrimSpace(string(raw))
	return e
}

// adaptRequest 把统一的 ChatRequest 翻译成 Anthropic 请求体。
func (p *Provider) adaptRequest(req llm.ChatRequest) (map[string]any, error) {
	// Anthropic 的 system 是独立字段，不在 messages 里；多条要拼起来。
	var system strings.Builder
	var normalMsgs []llm.Message
	for _, m := range req.Messages {
		if m.Role == llm.RoleSystem {
			if system.Len() > 0 {
				system.WriteString("\n\n")
			}
			system.WriteString(m.Content)
			continue
		}
		normalMsgs = append(normalMsgs, m)
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096 // Anthropic 要求 max_tokens 必填；1024 太容易截断
	}
	body := map[string]any{
		"model":      req.Model,
		"messages":   toAnthropicMessages(normalMsgs),
		"max_tokens": maxTokens,
	}
	if len(req.Tools) > 0 {
		body["tools"] = toAnthropicTool(req.Tools)
	}
	if req.Stream {
		body["stream"] = true
	}
	if system.Len() > 0 {
		body["system"] = system.String()
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	return body, nil
}

func (p *Provider) adaptResponse(body io.ReadCloser) (*llm.ChatResponse, error) {
	type UsageInfo struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	}
	type ClaudeResponse struct {
		ID           string         `json:"id"`
		Type         string         `json:"type"`
		Role         string         `json:"role"`
		Model        string         `json:"model"`
		Content      []contentBlock `json:"content"`
		StopReason   string         `json:"stop_reason"`
		StopSequence *string        `json:"stop_sequence"`
		Usage        UsageInfo      `json:"usage"`
	}
	var out ClaudeResponse
	if err := json.NewDecoder(body).Decode(&out); err != nil {
		return nil, err
	}
	resp := &llm.ChatResponse{}
	resp.InputTokens = out.Usage.InputTokens
	resp.OutputTokens = out.Usage.OutputTokens
	text, toolCalls := parseAnthropicResponse(out.Content)
	resp.Content = text
	resp.ToolCalls = toolCalls
	// tool_use 块目前被丢弃；如果模型只回了工具调用，Content 会是空串——
	// 明说一声，免得上层拿到空响应还以为是网络问题。
	if resp.Content == "" && len(resp.ToolCalls) == 0 && out.StopReason != "" {
		return resp, fmt.Errorf("%s: 无文本内容 (stop_reason=%s)", p.name, out.StopReason)
	}
	return resp, nil
}

// Chat claoud 非流式接口
/* 返回格式示例：
{
  "id": "msg_01XFDUDYJgA...",
  "type": "message",
  "role": "assistant",
  "model": "claude-3-opus-20240229",
  "content": [
    {
      "type": "text",
      "text": "Hello! How can I help you today?"
    }
  ],
  "stop_reason": "end_turn",
  "stop_sequence": null,
  "usage": {
    "input_tokens": 25,
    "output_tokens": 12
  }
}
*/
func (p *Provider) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	req.Stream = false // Chat 是非流式入口；流式走 ChatStream
	claudeReq, err := p.adaptRequest(req)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(claudeReq)
	if err != nil {
		return nil, fmt.Errorf("%s: 序列化请求: %w", p.name, err)
	}
	endpoint := strings.TrimRight(p.baseURL, "/")
	if !strings.HasSuffix(endpoint, "/v1") {
		endpoint += "/v1"
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint+"/messages",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, p.parseAPIError(resp)
	}
	return p.adaptResponse(resp.Body)
}

// errStreamDone 是 message_stop 的哨兵，用来让 ParseSSE 立刻收尾。
var errStreamDone = errors.New("stream done")

// ChatStream claude流式接口
/* 流式示例
Anthropic:
event: message_start
  data: {"type":"message_start","message":{"id":"msg_01...","type":"message","role":"assistant",
         "model":"claude-opus-5","content":[],"stop_reason":null,
         "usage":{"input_tokens":25,"output_tokens":1}}}

  event: content_block_start
  data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

  event: ping
  data: {"type":"ping"}

  event: content_block_delta
  data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"你"}}

  event: content_block_delta
  data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"好"}}

  event: content_block_stop
  data: {"type":"content_block_stop","index":0}

  event: message_delta
  data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},
         "usage":{"output_tokens":15}}

  event: message_stop
  data: {"type":"message_stop"}
*/
//
// 调用方必须把返回的 channel 读完，或者 cancel ctx。提前 break 且不 cancel 会让
// 内部 goroutine 永久阻塞在发送上，连底层连接一起泄漏。
func (p *Provider) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	req.Stream = true
	claudeReq, err := p.adaptRequest(req)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(claudeReq)
	if err != nil {
		return nil, fmt.Errorf("%s: 序列化请求: %w", p.name, err)
	}
	endpoint := strings.TrimRight(p.baseURL, "/")
	if !strings.HasSuffix(endpoint, "/v1") {
		endpoint += "/v1"
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint+"/messages",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

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
			ev, done, err := p.parseClaudeStreamEvent(data)
			if err != nil {
				return err
			}
			if done {
				return errStreamDone
			}
			switch ev.Type {
			case "text":
				if ev.Content != "" {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case out <- llm.StreamChunk{Content: ev.Content}:
						return nil
					}
				}
			case "tool_start":
				ptc, ok := toolCallsMap[ev.ToolCallIdx]
				if !ok {
					ptc = &partialToolCall{}
					toolCallsMap[ev.ToolCallIdx] = ptc
					if ev.ToolCallIdx > maxIndex {
						maxIndex = ev.ToolCallIdx
					}
				}
				ptc.id = ev.ToolCallID
				ptc.name = ev.ToolCallName
			case "tool_delta":
				ptc, ok := toolCallsMap[ev.ToolCallIdx]
				if !ok {
					ptc = &partialToolCall{}
					toolCallsMap[ev.ToolCallIdx] = ptc
					if ev.ToolCallIdx > maxIndex {
						maxIndex = ev.ToolCallIdx
					}
				}
				ptc.args.WriteString(ev.PartialJSON)
			}
			return nil
		})
		switch {
		case errors.Is(streamErr, errStreamDone):
			streamErr = nil // 正常收尾
		case streamErr == nil:
			streamErr = fmt.Errorf("%s: 流在 message_stop 前结束", p.name)
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

type claudeStreamEvent struct {
	Type         string
	Content      string
	ToolCallIdx  int
	ToolCallID   string
	ToolCallName string
	PartialJSON  string
}

func (p *Provider) parseClaudeStreamEvent(data []byte) (ev claudeStreamEvent, done bool, err error) {
	var raw struct {
		Type         string `json:"type"`
		Index        int    `json:"index"`
		ContentBlock *struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"content_block"`
		Delta *struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			PartialJSON string `json:"partial_json"`
		} `json:"delta"`
		Error *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return claudeStreamEvent{}, false, fmt.Errorf("%s: 解析流事件失败: %w (原文 %.200q)", p.name, err, data)
	}
	switch raw.Type {
	case "error":
		if raw.Error != nil {
			return claudeStreamEvent{}, false, fmt.Errorf("%s: 流内错误 %s: %s", p.name, raw.Error.Type, raw.Error.Message)
		}
		return claudeStreamEvent{}, false, fmt.Errorf("%s: 流内错误 (原文 %.200q)", p.name, data)
	case "message_stop":
		return claudeStreamEvent{}, true, nil
	case "content_block_start":
		if raw.ContentBlock != nil && raw.ContentBlock.Type == "tool_use" {
			return claudeStreamEvent{
				Type:         "tool_start",
				ToolCallIdx:  raw.Index,
				ToolCallID:   raw.ContentBlock.ID,
				ToolCallName: raw.ContentBlock.Name,
			}, false, nil
		}
		return claudeStreamEvent{}, false, nil
	case "content_block_delta":
		if raw.Delta != nil {
			if raw.Delta.Type == "text_delta" {
				return claudeStreamEvent{
					Type:    "text",
					Content: raw.Delta.Text,
				}, false, nil
			}
			if raw.Delta.Type == "input_json_delta" {
				return claudeStreamEvent{
					Type:        "tool_delta",
					ToolCallIdx: raw.Index,
					PartialJSON: raw.Delta.PartialJSON,
				}, false, nil
			}
		}
		return claudeStreamEvent{}, false, nil
	default:
		return claudeStreamEvent{}, false, nil
	}
}

// parseClaudeDelta 解析 Anthropic 流事件。
// 与 OpenAI 的两点不同：结束标记是 message_stop（不是 [DONE]），
// 且事件种类要靠 data 里的 type 字段区分——ParseSSE 只交出 data，不交 event 行。
func (p *Provider) parseClaudeDelta(data []byte) (delta string, done bool, err error) {
	ev, done, err := p.parseClaudeStreamEvent(data)
	if err != nil || done {
		return "", done, err
	}
	if ev.Type == "text" {
		return ev.Content, false, nil
	}
	return "", false, nil
}
