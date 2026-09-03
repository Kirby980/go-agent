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
	"test/agent/internal/llm"
	"test/agent/internal/transport"
)

type Provider struct {
	name    string
	baseURL string
	apiKey  string
	client  *transport.Client
}

type Config struct {
	Name    string
	BaseURL string
	APIKey  string
}

// New 创建一个 OpenAI 兼容 Provider。
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
	return llm.Capability{Streaming: true}
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
	msgs := make([]map[string]string, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role == llm.RoleSystem {
			if system.Len() > 0 {
				system.WriteString("\n\n") // 没有分隔符会粘成一句话
			}
			system.WriteString(m.Content)
			continue
		}
		if m.Role != llm.RoleUser && m.Role != llm.RoleAssistant {
			return nil, fmt.Errorf("%s: M02 尚未实现 role %q 的协议转换", p.name, m.Role)
		}
		msgs = append(msgs, map[string]string{
			"role":    string(m.Role),
			"content": m.Content,
		})
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096 // Anthropic 要求 max_tokens 必填；1024 太容易截断
	}
	body := map[string]any{
		"model":      req.Model,
		"messages":   msgs,
		"max_tokens": maxTokens,
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
	type ContentBlock struct {
		Type  string      `json:"type"`            // "text" 或 "tool_use"
		Text  string      `json:"text,omitempty"`  // 当 type == "text"
		ID    string      `json:"id,omitempty"`    // 当 type == "tool_use"
		Name  string      `json:"name,omitempty"`  // 当 type == "tool_use"
		Input interface{} `json:"input,omitempty"` // 当 type == "tool_use"
	}

	type UsageInfo struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	}
	type ClaudeResponse struct {
		ID           string         `json:"id"`
		Type         string         `json:"type"`
		Role         string         `json:"role"`
		Model        string         `json:"model"`
		Content      []ContentBlock `json:"content"`
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
	text := strings.Builder{}
	for _, v := range out.Content {
		if v.Type == "text" {
			text.WriteString(v.Text)
		}
	}
	resp.Content = text.String()
	// tool_use 块目前被丢弃；如果模型只回了工具调用，Content 会是空串——
	// 明说一声，免得上层拿到空响应还以为是网络问题。
	if resp.Content == "" && out.StopReason != "" {
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
		streamErr := transport.ParseSSE(resp.Body, func(data []byte) error {
			delta, done, err := p.parseClaudeDelta(data)
			if err != nil {
				return err
			}
			if done {
				return errStreamDone
			}
			if delta == "" {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case out <- llm.StreamChunk{Content: delta}:
				return nil
			}
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
		}
	}()
	return out, nil
}

// parseClaudeDelta 解析 Anthropic 流事件。
// 与 OpenAI 的两点不同：结束标记是 message_stop（不是 [DONE]），
// 且事件种类要靠 data 里的 type 字段区分——ParseSSE 只交出 data，不交 event 行。
func (p *Provider) parseClaudeDelta(data []byte) (delta string, done bool, err error) {
	var ev struct {
		Type  string `json:"type"`
		Delta struct {
			Type string `json:"type"` // text_delta / thinking_delta / input_json_delta
			Text string `json:"text"`
		} `json:"delta"`
		// 流开始后才出错（overloaded_error 等）只能走这里，HTTP 状态码已经是 200 了。
		Error *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return "", false, fmt.Errorf("%s: 解析流事件失败: %w (原文 %.200q)", p.name, err, data)
	}
	switch ev.Type {
	case "error":
		if ev.Error != nil {
			return "", false, fmt.Errorf("%s: 流内错误 %s: %s", p.name, ev.Error.Type, ev.Error.Message)
		}
		return "", false, fmt.Errorf("%s: 流内错误 (原文 %.200q)", p.name, data)
	case "message_stop":
		return "", true, nil
	case "content_block_delta":
		// thinking_delta / input_json_delta 是思考和工具入参，M02 的统一结构还接不住，先忽略
		if ev.Delta.Type == "text_delta" {
			return ev.Delta.Text, false, nil
		}
		return "", false, nil
	default:
		// ping / message_start / content_block_start / content_block_stop / message_delta
		return "", false, nil
	}
}
