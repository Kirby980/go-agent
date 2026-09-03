package claude

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"test/agent/internal/llm"
)

func mkResp(status int, body string, hdr http.Header) *http.Response {
	if hdr == nil {
		hdr = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Header:     hdr,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestParseAPIError(t *testing.T) {
	p := New(Config{Name: "claude"})

	// 标准 Anthropic 错误体，request_id 在 body 里
	err := p.parseAPIError(mkResp(400,
		`{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens required"},"request_id":"req_011C"}`,
		http.Header{"Request-Id": []string{"从头里来的"}}))
	var apiErr *llm.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("想要 *llm.APIError，得到 %T", err)
	}
	if apiErr.Status != 400 || apiErr.Type != "invalid_request_error" ||
		apiErr.Message != "max_tokens required" || apiErr.RequestID != "req_011C" {
		t.Fatalf("解析错了: %+v", apiErr) // body 里的 request_id 应盖过响应头
	}

	// body 没有 request_id 时回落到响应头
	err = p.parseAPIError(mkResp(401,
		`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`,
		http.Header{"Request-Id": []string{"req-hdr"}}))
	errors.As(err, &apiErr)
	if apiErr.RequestID != "req-hdr" {
		t.Fatalf("request_id 没回落到响应头: %+v", apiErr)
	}

	// 非 JSON（网关返回的 HTML 之类）也不能丢信息
	err = p.parseAPIError(mkResp(529, "overloaded", nil))
	errors.As(err, &apiErr)
	if apiErr.Status != 529 || apiErr.Message != "overloaded" {
		t.Fatalf("非 JSON body 丢了: %+v", apiErr)
	}
}

func TestAdaptRequestJoinsSystem(t *testing.T) {
	p := New(Config{Name: "claude"})
	body, err := p.adaptRequest(llm.ChatRequest{
		Model: "claude-opus-5",
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "你是助手"},
			{Role: llm.RoleSystem, Content: "回答要简洁"},
			{Role: llm.RoleUser, Content: "你好"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := body["system"]; got != "你是助手\n\n回答要简洁" {
		t.Fatalf("多条 system 拼接错误: %q", got)
	}
	if n := len(body["messages"].([]map[string]string)); n != 1 {
		t.Fatalf("system 不该留在 messages 里，剩下 %d 条", n)
	}
}

func TestParseClaudeDelta(t *testing.T) {
	p := New(Config{Name: "claude"})

	// 正常文本增量
	delta, done, err := p.parseClaudeDelta([]byte(
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`))
	if err != nil || done || delta != "Hi" {
		t.Fatalf("正常增量: delta=%q done=%v err=%v", delta, done, err)
	}

	// 结束标记是 message_stop，不是 [DONE]
	if _, done, err = p.parseClaudeDelta([]byte(`{"type":"message_stop"}`)); err != nil || !done {
		t.Fatalf("message_stop 没识别: done=%v err=%v", done, err)
	}

	// 骨架事件全部安静跳过
	for _, ev := range []string{
		`{"type":"ping"}`,
		`{"type":"message_start","message":{"id":"msg_1"}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
		// thinking_delta / input_json_delta：M02 接不住，忽略但不能当错误
		`{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"..."}}`,
	} {
		if delta, done, err = p.parseClaudeDelta([]byte(ev)); err != nil || done || delta != "" {
			t.Fatalf("%s 应被忽略: delta=%q done=%v err=%v", ev, delta, done, err)
		}
	}

	// 流内错误必须冒出来
	_, _, err = p.parseClaudeDelta([]byte(
		`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))
	if err == nil || !strings.Contains(err.Error(), "Overloaded") {
		t.Fatalf("流内 error 没冒出来: %v", err)
	}
}

func TestChatStreamEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Errorf("anthropic-version = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		// 带 event: 行，验证 ParseSSE 只取 data 且能靠 type 字段分派
		io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\"}\n\n")
		io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"你\"}}\n\n")
		io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"好\"}}\n\n")
		io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		io.WriteString(w, ": keep-alive\n\n") // message_stop 之后仍有心跳
	}))
	defer srv.Close()

	p := New(Config{Name: "claude", BaseURL: srv.URL})
	ch, err := p.ChatStream(context.Background(), llm.ChatRequest{
		Model:    "claude-opus-5",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	for chunk := range ch { // 卡在这里说明 message_stop 早退没生效
		if chunk.Err != nil {
			t.Fatalf("不该有错误: %v", chunk.Err)
		}
		got.WriteString(chunk.Content)
	}
	if got.String() != "你好" {
		t.Fatalf("拼接结果错误: %q", got.String())
	}
}
