package patterns

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Kirby980/agent/llm"
)

type mockLLM struct {
	chatFunc func(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error)
}

func (m *mockLLM) Name() string                 { return "mock" }
func (m *mockLLM) Capabilities() llm.Capability { return llm.Capability{} }
func (m *mockLLM) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	if m.chatFunc != nil {
		return m.chatFunc(ctx, req)
	}
	return &llm.ChatResponse{Content: ""}, nil
}
func (m *mockLLM) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return nil, errors.New("stream not supported")
}

func TestIntentRouter_Dispatch(t *testing.T) {
	mock := &mockLLM{
		chatFunc: func(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
			for _, msg := range req.Messages {
				if strings.Contains(msg.Content, "今天天气怎么样") {
					return &llm.ChatResponse{Content: `{"route":"general_answer"}`}, nil
				}
				if strings.Contains(msg.Content, "func main()") {
					return &llm.ChatResponse{Content: "```json\n{\"route\":\"code_review\"}\n```"}, nil
				}
			}
			return &llm.ChatResponse{Content: `{"route":"unknown"}`}, nil
		},
	}

	router := &IntentRouter{
		Provider: mock,
		Model:    "test-model",
		Routes: []Route{
			{
				Name:        "code_review",
				Description: "代码审查",
				Handle: func(ctx context.Context, input string) (string, error) {
					return "handled by code_review", nil
				},
			},
			{
				Name:        "general_answer",
				Description: "普通回答",
				Handle: func(ctx context.Context, input string) (string, error) {
					return "handled by general_answer", nil
				},
			},
		},
		Fallback: func(ctx context.Context, input string) (string, error) {
			return "handled by fallback", nil
		},
	}

	ctx := context.Background()

	res1, err := router.Dispatch(ctx, "今天天气怎么样")
	if err != nil || res1 != "handled by general_answer" {
		t.Fatalf("expected general_answer, got res=%q, err=%v", res1, err)
	}

	res2, err := router.Dispatch(ctx, "func main() {}")
	if err != nil || res2 != "handled by code_review" {
		t.Fatalf("expected code_review, got res=%q, err=%v", res2, err)
	}

	res3, err := router.Dispatch(ctx, "未知意图")
	if err != nil || res3 != "handled by fallback" {
		t.Fatalf("expected fallback, got res=%q, err=%v", res3, err)
	}
}

func TestPGE_NonCodeInput(t *testing.T) {
	mock := &mockLLM{
		chatFunc: func(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
			systemMsg := ""
			userMsg := ""
			for _, msg := range req.Messages {
				if msg.Role == llm.RoleSystem {
					systemMsg = msg.Content
				}
				if msg.Role == llm.RoleUser {
					userMsg = msg.Content
				}
			}

			if strings.Contains(systemMsg, "你是意图分类器") {
				if strings.Contains(userMsg, "李白是谁") {
					return &llm.ChatResponse{Content: `{"route":"general_answer"}`}, nil
				}
			}

			if strings.Contains(systemMsg, "人工智能技术助手") {
				return &llm.ChatResponse{Content: "李白是唐代著名浪漫主义诗人，字太白，号青莲居士。"}, nil
			}

			return &llm.ChatResponse{Content: "unexpected"}, nil
		},
	}

	ctx := context.Background()
	ans, err := PGE(ctx, mock, "test-model", "李白是谁", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(ans, "李白是唐代著名浪漫主义诗人") {
		t.Fatalf("expected general answer, got: %s", ans)
	}
}
