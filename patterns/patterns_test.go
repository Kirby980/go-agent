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

func TestGateway(t *testing.T) {
	mock := &mockLLM{
		chatFunc: func(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
			systemMsg := ""
			userMsg := ""
			for _, m := range req.Messages {
				if m.Role == llm.RoleSystem {
					systemMsg = m.Content
				}
				if m.Role == llm.RoleUser {
					userMsg = m.Content
				}
			}

			// 意图分类器
			if strings.Contains(systemMsg, "你是意图分类器") {
				if strings.Contains(userMsg, "几点了") {
					return &llm.ChatResponse{Content: `{"route":"single_agent"}`}, nil
				}
				if strings.Contains(userMsg, "func criticalWork()") {
					return &llm.ChatResponse{Content: `{"route":"multi_agent_pge"}`}, nil
				}
				if strings.Contains(userMsg, "复合大任务") {
					return &llm.ChatResponse{Content: `{"route":"multi_agent_orchestrator"}`}, nil
				}
			}

			// 单 Agent 回答（由于 mockLLM 未开启 Tools，Agent 会自动降级走 ReAct 模式，因此返回 Final Answer）
			if strings.Contains(systemMsg, "命令行 AI 助手") || strings.Contains(userMsg, "几点了") {
				return &llm.ChatResponse{Content: "Thought: 用户在询问时间\nFinal Answer: 现在是下午三点"}, nil
			}

			// PGE 审查流程：Planner
			if strings.Contains(systemMsg, "代码意图分类器") {
				return &llm.ChatResponse{Content: `{"dimensions":["正确性"]}`}, nil
			}
			// PGE 审查流程：Generator
			if strings.Contains(systemMsg, "代码审查专家") {
				return &llm.ChatResponse{Content: `{"dimension":"正确性","review":"逻辑严密，无并发问题"}`}, nil
			}
			// PGE 审查流程：Evaluator 整合
			if strings.Contains(systemMsg, "代码审查报告整合专家") {
				return &llm.ChatResponse{Content: "最终审查报告：通过"}, nil
			}
			// PGE 审查流程：Evaluator 评估打分
			if strings.Contains(systemMsg, "评估给出的代码审查报告") {
				return &llm.ChatResponse{Content: `{"pass":true,"score":95,"feedback":""}`}, nil
			}

			// Orchestrator 分解与综合
			if strings.Contains(systemMsg, "任务编排者") {
				return &llm.ChatResponse{Content: `{"subtasks":[{"id":"t1","description":"子任务1"}]}`}, nil
			}
			if strings.Contains(systemMsg, "综合成对原始任务的完整") {
				return &llm.ChatResponse{Content: "复合大任务综合完成"}, nil
			}
			if strings.Contains(userMsg, "子任务1") {
				return &llm.ChatResponse{Content: "Thought: 执行子任务1\nFinal Answer: 子任务1完成"}, nil
			}

			return &llm.ChatResponse{Content: "ok"}, nil
		},
	}

	gw := NewGateway(mock, "test-model", nil)
	ctx := context.Background()

	// 1. 测试路由至 single_agent
	res1, route1, err := gw.ExecuteWithRoute(ctx, "现在几点了？")
	if err != nil || route1 != "single_agent" || !strings.Contains(res1, "下午三点") {
		t.Fatalf("single_agent failed: route=%q, res=%q, err=%v", route1, res1, err)
	}

	// 2. 测试路由至 multi_agent_pge
	res2, route2, err := gw.ExecuteWithRoute(ctx, "请审查以下代码：func criticalWork() {}")
	if err != nil || route2 != "multi_agent_pge" || !strings.Contains(res2, "最终审查报告") {
		t.Fatalf("multi_agent_pge failed: route=%q, res=%q, err=%v", route2, res2, err)
	}

	// 3. 测试路由至 multi_agent_orchestrator
	res3, route3, err := gw.ExecuteWithRoute(ctx, "这是一个复合大任务，包含很多模块")
	if err != nil || route3 != "multi_agent_orchestrator" || !strings.Contains(res3, "复合大任务综合完成") {
		t.Fatalf("multi_agent_orchestrator failed: route=%q, res=%q, err=%v", route3, res3, err)
	}
}
