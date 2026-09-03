package openai

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
	p := New(Config{Name: "openai"})

	// 标准 OpenAI 错误体
	hdr := http.Header{"X-Request-Id": []string{"req-abc"}}
	err := p.parseAPIError(mkResp(400, `{"error":{"message":"bad model","type":"invalid_request_error"}}`, hdr))
	var apiErr *llm.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("想要 *llm.APIError，得到 %T", err)
	}
	if apiErr.Status != 400 || apiErr.Type != "invalid_request_error" ||
		apiErr.Message != "bad model" || apiErr.RequestID != "req-abc" {
		t.Fatalf("解析错了: %+v", apiErr)
	}

	// type 缺失时回落到 code
	err = p.parseAPIError(mkResp(429, `{"error":{"message":"slow down","code":"rate_limit"}}`, nil))
	errors.As(err, &apiErr)
	if apiErr.Type != "rate_limit" {
		t.Fatalf("type 没回落到 code: %+v", apiErr)
	}

	// 非 JSON（网关返回的 HTML 之类）也不能丢信息
	err = p.parseAPIError(mkResp(502, "<html>bad gateway</html>", nil))
	errors.As(err, &apiErr)
	if apiErr.Status != 502 || !strings.Contains(apiErr.Message, "bad gateway") {
		t.Fatalf("非 JSON body 丢了: %+v", apiErr)
	}
}

func TestParseOpenAIDelta(t *testing.T) {
	p := New(Config{Name: "openai"})

	// 正常增量
	delta, done, err := p.parseOpenAIDelta([]byte(`{"choices":[{"delta":{"content":"Hi"}}]}`))
	if err != nil || done || delta != "Hi" {
		t.Fatalf("正常增量: delta=%q done=%v err=%v", delta, done, err)
	}

	// 结束标记
	if _, done, err = p.parseOpenAIDelta([]byte("[DONE]")); err != nil || !done {
		t.Fatalf("[DONE] 没识别: done=%v err=%v", done, err)
	}

	// 只带 usage 的收尾事件，不是错误
	if delta, done, err = p.parseOpenAIDelta([]byte(`{"choices":[],"usage":{"total_tokens":9}}`)); err != nil || done || delta != "" {
		t.Fatalf("usage 事件被误判: delta=%q done=%v err=%v", delta, done, err)
	}

	// 流内错误必须冒出来，不能当成 usage 事件吞掉
	_, _, err = p.parseOpenAIDelta([]byte(`{"error":{"message":"upstream timeout","type":"server_error"}}`))
	if err == nil {
		t.Fatal("流内 error 被吞了")
	}
	if !strings.Contains(err.Error(), "upstream timeout") {
		t.Fatalf("错误没带上原因: %v", err)
	}

	// 坏 JSON 要带上原文，方便查是哪家网关的格式不对
	if _, _, err = p.parseOpenAIDelta([]byte("{not json")); err == nil ||
		!strings.Contains(err.Error(), "not json") {
		t.Fatalf("坏 JSON 没带原文: %v", err)
	}
}

func TestChatStreamEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\n")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
		// [DONE] 之后还发心跳：解析器必须已经收尾，否则调用方的 range 永远不结束
		io.WriteString(w, ": keep-alive\n\n")
	}))
	defer srv.Close()

	p := New(Config{Name: "openai", BaseURL: srv.URL})
	ch, err := p.ChatStream(context.Background(), llm.ChatRequest{
		Model:    "gpt-4o",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	for chunk := range ch { // 卡在这里说明 [DONE] 早退没生效
		if chunk.Err != nil {
			t.Fatalf("不该有错误: %v", chunk.Err)
		}
		got.WriteString(chunk.Content)
	}
	if got.String() != "你好" {
		t.Fatalf("拼接结果错误: %q", got.String())
	}
}
