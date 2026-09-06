package router

import (
	"context"
	"errors"
	"testing"

	"github.com/Kirby980/agent/llm"
)

type mockProvider struct {
	name string
	err  error
	resp string
}

func (m *mockProvider) Name() string                 { return m.name }
func (m *mockProvider) Capabilities() llm.Capability { return llm.Capability{} }
func (m *mockProvider) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &llm.ChatResponse{Content: m.resp}, nil
}
func (m *mockProvider) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan llm.StreamChunk, 1)
	ch <- llm.StreamChunk{Content: m.resp}
	close(ch)
	return ch, nil
}

func TestRouter_Priority_Fallback(t *testing.T) {
	p1 := &mockProvider{name: "p1", err: errors.New("p1 down")}
	p2 := &mockProvider{name: "p2", resp: "hello from p2"}

	r, err := New(Priority{}, p1, p2)
	if err != nil {
		t.Fatalf("unexpected init error: %v", err)
	}

	resp, name, err := r.Chat(context.Background(), llm.ChatRequest{})
	if err != nil {
		t.Fatalf("expected fallback to p2, but got error: %v", err)
	}
	if name != "p2" {
		t.Errorf("expected responder p2, got %s", name)
	}
	if resp.Content != "hello from p2" {
		t.Errorf("expected 'hello from p2', got %s", resp.Content)
	}
}

func TestRouter_AllFailed(t *testing.T) {
	p1 := &mockProvider{name: "p1", err: errors.New("err1")}
	p2 := &mockProvider{name: "p2", err: errors.New("err2")}

	r, err := New(Priority{}, p1, p2)
	if err != nil {
		t.Fatalf("init error: %v", err)
	}

	_, _, err = r.Chat(context.Background(), llm.ChatRequest{})
	if err == nil {
		t.Fatalf("expected error when all providers fail")
	}
}

func TestRouter_RoundRobin(t *testing.T) {
	p1 := &mockProvider{name: "p1", resp: "resp1"}
	p2 := &mockProvider{name: "p2", resp: "resp2"}

	r, err := New(NewRoundRobin(), p1, p2)
	if err != nil {
		t.Fatalf("init error: %v", err)
	}

	// 第 1 次调用期望命中 p1
	resp1, name1, err := r.Chat(context.Background(), llm.ChatRequest{})
	if err != nil || name1 != "p1" || resp1.Content != "resp1" {
		t.Fatalf("call 1 failed: name=%s, err=%v", name1, err)
	}

	// 第 2 次调用期望轮询命中 p2
	resp2, name2, err := r.Chat(context.Background(), llm.ChatRequest{})
	if err != nil || name2 != "p2" || resp2.Content != "resp2" {
		t.Fatalf("call 2 failed: name=%s, err=%v", name2, err)
	}

	// 第 3 次调用期望重新轮询回 p1
	resp3, name3, err := r.Chat(context.Background(), llm.ChatRequest{})
	if err != nil || name3 != "p1" || resp3.Content != "resp1" {
		t.Fatalf("call 3 failed: name=%s, err=%v", name3, err)
	}
}

func TestRouter_AsProvider(t *testing.T) {
	p1 := &mockProvider{name: "p1", resp: "from p1"}
	r, err := New(Priority{}, p1)
	if err != nil {
		t.Fatalf("init error: %v", err)
	}

	provider := r.AsProvider("test-cluster")
	if provider.Name() != "test-cluster" {
		t.Errorf("expected name test-cluster, got %s", provider.Name())
	}

	resp, err := provider.Chat(context.Background(), llm.ChatRequest{})
	if err != nil || resp.Content != "from p1" {
		t.Fatalf("AsProvider.Chat failed: resp=%v, err=%v", resp, err)
	}
}

