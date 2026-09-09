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

type trackingProvider struct {
	tools       bool
	streaming   bool
	chatFunc    func(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error)
	streamFunc  func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error)
	chatCalls   int
	streamCalls int
}

func (t *trackingProvider) Name() string { return "tracking-mock" }
func (t *trackingProvider) Capabilities() llm.Capability {
	return llm.Capability{Tools: t.tools, Streaming: t.streaming}
}
func (t *trackingProvider) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	t.chatCalls++
	if t.chatFunc != nil {
		return t.chatFunc(ctx, req)
	}
	return &llm.ChatResponse{Content: "chat reply"}, nil
}
func (t *trackingProvider) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	t.streamCalls++
	if t.streamFunc != nil {
		return t.streamFunc(ctx, req)
	}
	ch := make(chan llm.StreamChunk, 2)
	ch <- llm.StreamChunk{Content: "stream reply"}
	close(ch)
	return ch, nil
}

func TestRun_NonStreaming_FunctionCalling(t *testing.T) {
	p := &trackingProvider{tools: true, streaming: true}
	ag := New(p, "test-model", tool.NewRegistry())

	ans, err := ag.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ans != "chat reply" {
		t.Errorf("expected 'chat reply', got %q", ans)
	}
	if p.chatCalls != 1 {
		t.Errorf("expected 1 Chat call, got %d", p.chatCalls)
	}
	if p.streamCalls != 0 {
		t.Errorf("expected 0 ChatStream calls, got %d", p.streamCalls)
	}
}

func TestRunStream_Streaming_FunctionCalling(t *testing.T) {
	p := &trackingProvider{tools: true, streaming: true}
	p.streamFunc = func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
		ch := make(chan llm.StreamChunk, 3)
		ch <- llm.StreamChunk{Content: "Hello "}
		ch <- llm.StreamChunk{Content: "World"}
		close(ch)
		return ch, nil
	}
	ag := New(p, "test-model", tool.NewRegistry())

	var deltas []string
	doneSeen := false
	for ev := range ag.RunStream(context.Background(), "hello") {
		switch ev.Type {
		case EventAnswerDelta:
			deltas = append(deltas, ev.Text)
		case EventDone:
			doneSeen = true
		}
	}

	if p.streamCalls != 1 {
		t.Errorf("expected 1 ChatStream call, got %d", p.streamCalls)
	}
	if p.chatCalls != 0 {
		t.Errorf("expected 0 Chat calls, got %d", p.chatCalls)
	}
	if strings.Join(deltas, "") != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", strings.Join(deltas, ""))
	}
	if !doneSeen {
		t.Errorf("expected EventDone to be emitted")
	}
}

func TestRun_NonStreaming_ReAct(t *testing.T) {
	p := &trackingProvider{tools: false, streaming: true}
	p.chatFunc = func(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
		return &llm.ChatResponse{Content: "Thought: I know the answer\nFinal Answer: 42"}, nil
	}
	ag := New(p, "test-model", tool.NewRegistry())

	ans, err := ag.Run(context.Background(), "what is the answer?")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ans != "42" {
		t.Errorf("expected '42', got %q", ans)
	}
	if p.chatCalls != 1 {
		t.Errorf("expected 1 Chat call, got %d", p.chatCalls)
	}
	if p.streamCalls != 0 {
		t.Errorf("expected 0 ChatStream calls, got %d", p.streamCalls)
	}
}

func TestRunStream_Streaming_ReAct(t *testing.T) {
	p := &trackingProvider{tools: false, streaming: true}
	p.streamFunc = func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
		ch := make(chan llm.StreamChunk, 5)
		ch <- llm.StreamChunk{Content: "Thought: I am thinking\n"}
		ch <- llm.StreamChunk{Content: "Final Answer: "}
		ch <- llm.StreamChunk{Content: "4"}
		ch <- llm.StreamChunk{Content: "2"}
		close(ch)
		return ch, nil
	}
	ag := New(p, "test-model", tool.NewRegistry())

	var deltas []string
	var thoughts []string
	doneSeen := false
	for ev := range ag.RunStream(context.Background(), "what is the answer?") {
		switch ev.Type {
		case EventAnswerDelta:
			deltas = append(deltas, ev.Text)
		case EventThought:
			thoughts = append(thoughts, ev.Text)
		case EventDone:
			doneSeen = true
		}
	}

	if p.streamCalls != 1 {
		t.Errorf("expected 1 ChatStream call, got %d", p.streamCalls)
	}
	if p.chatCalls != 0 {
		t.Errorf("expected 0 Chat calls, got %d", p.chatCalls)
	}
	if strings.Join(deltas, "") != "42" {
		t.Errorf("expected '42', got %q", strings.Join(deltas, ""))
	}
	if !doneSeen {
		t.Errorf("expected EventDone to be emitted")
	}
}

func TestRunStream_FallbackWhenNoStreamingCapability(t *testing.T) {
	p := &trackingProvider{tools: false, streaming: false} // streaming capability disabled
	p.chatFunc = func(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
		return &llm.ChatResponse{Content: "Thought: fallback\nFinal Answer: 99"}, nil
	}
	ag := New(p, "test-model", tool.NewRegistry())

	var deltas []string
	for ev := range ag.RunStream(context.Background(), "test fallback") {
		if ev.Type == EventAnswerDelta {
			deltas = append(deltas, ev.Text)
		}
	}

	if p.chatCalls != 1 {
		t.Errorf("expected 1 Chat call (fallback), got %d", p.chatCalls)
	}
	if p.streamCalls != 0 {
		t.Errorf("expected 0 ChatStream calls, got %d", p.streamCalls)
	}
	if strings.Join(deltas, "") != "99" {
		t.Errorf("expected '99', got %q", strings.Join(deltas, ""))
	}
}
