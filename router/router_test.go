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
