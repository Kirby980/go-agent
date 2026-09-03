package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Kirby980/agent/llm"
	"github.com/Kirby980/agent/tool"
)

type mockProvider struct {
	tools bool
}

func (m *mockProvider) Name() string                     { return "mock" }
func (m *mockProvider) Capabilities() llm.Capability     { return llm.Capability{Tools: m.tools} }
func (m *mockProvider) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Content: "mock reply"}, nil
}
func (m *mockProvider) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

type dummyTool struct{}

func (d *dummyTool) Name() string                               { return "dummy" }
func (d *dummyTool) Description() string                        { return "a dummy tool" }
func (d *dummyTool) Parameters() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (d *dummyTool) Call(_ context.Context, _ json.RawMessage) (string, error) {
	return "dummy result", nil
}

func TestInitialState_FunctionCalling(t *testing.T) {
	p := &mockProvider{tools: true}
	reg := tool.NewRegistry(&dummyTool{})
	ag := New(p, "mock-model", reg, WithSystemPrompt("自定义系统提示词"))

	ctx := context.Background()
	st, err := ag.initialState(ctx, "测试目标")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if st.Goal != "测试目标" {
		t.Errorf("got goal %q, want %q", st.Goal, "测试目标")
	}
	if len(st.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(st.Messages))
	}
	if st.Messages[0].Role != llm.RoleSystem || st.Messages[0].Content != "自定义系统提示词" {
		t.Errorf("unexpected system message: %+v", st.Messages[0])
	}
	if st.Messages[1].Role != llm.RoleUser || st.Messages[1].Content != "测试目标" {
		t.Errorf("unexpected user message: %+v", st.Messages[1])
	}
	if ag.memory != st {
		t.Errorf("agent.memory was not updated")
	}
}

func TestInitialState_ReAct(t *testing.T) {
	p := &mockProvider{tools: false}
	reg := tool.NewRegistry(&dummyTool{})
	ag := New(p, "mock-model", reg)

	ctx := context.Background()
	st, err := ag.initialState(ctx, "ReAct测试")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(st.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(st.Messages))
	}
	if !strings.Contains(st.Messages[0].Content, "dummy: a dummy tool") {
		t.Errorf("expected tool description in react system prompt, got: %s", st.Messages[0].Content)
	}
	if !strings.Contains(st.Messages[0].Content, "Action Input:") {
		t.Errorf("expected react template in system prompt, got: %s", st.Messages[0].Content)
	}
}

func TestInitialState_WithStore(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)
	p := &mockProvider{tools: true}
	reg := tool.NewRegistry()

	ctx := context.Background()
	sessionID := "test-session"
	ag := New(p, "mock-model", reg, WithStore(store, sessionID))

	// First run: store has no file yet, should create new
	st1, err := ag.initialState(ctx, "第一轮目标")
	if err != nil {
		t.Fatalf("first run error: %v", err)
	}
	if len(st1.Messages) != 2 {
		t.Fatalf("first run got %d messages, want 2", len(st1.Messages))
	}

	// Save checkpoint
	st1.Messages = append(st1.Messages, llm.Message{Role: llm.RoleAssistant, Content: "第一轮回答"})
	ag.checkpoint(ctx, st1)

	// Second run with a new Agent instance sharing the store and sessionID
	ag2 := New(p, "mock-model", reg, WithStore(store, sessionID))
	st2, err := ag2.initialState(ctx, "第二轮目标")
	if err != nil {
		t.Fatalf("second run error: %v", err)
	}

	if st2.Goal != "第二轮目标" {
		t.Errorf("got goal %q, want %q", st2.Goal, "第二轮目标")
	}
	// Messages should have: system prompt, first goal, assistant reply, second goal
	if len(st2.Messages) != 4 {
		t.Fatalf("second run got %d messages, want 4", len(st2.Messages))
	}
	if st2.Messages[3].Role != llm.RoleUser || st2.Messages[3].Content != "第二轮目标" {
		t.Errorf("unexpected message 3: %+v", st2.Messages[3])
	}
}
