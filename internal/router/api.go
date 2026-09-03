package router

import (
	"context"
	"fmt"
	"test/agent/internal/llm"
)

type Router struct {
	providers []llm.Provider
	strategy  Strategy
}

func New(strategy Strategy, providers ...llm.Provider) (*Router, error) {
	if strategy == nil {
		return nil, fmt.Errorf("router strategy 不能为空")
	}
	if len(providers) == 0 {
		return nil, fmt.Errorf("至少需要一个 Provider")
	}
	for i, p := range providers {
		if p == nil {
			return nil, fmt.Errorf("providers[%d] 不能为空", i)
		}
	}
	return &Router{
		providers: append([]llm.Provider(nil), providers...),
		strategy:  strategy,
	}, nil
}

func (r *Router) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, string, error) {
	var lastErr error
	for _, p := range r.strategy.Order(r.providers) {
		resp, err := p.Chat(ctx, req)
		if err == nil {
			return resp, p.Name(), nil // 第二个返回值告诉上层“谁回答的”
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, "", ctx.Err() // 用户取消，不再尝试
		}
	}
	return nil, "", fmt.Errorf("所有 Provider 均失败: %w", lastErr)
}

func (r *Router) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, string, error) {
	var lastErr error
	for _, p := range r.strategy.Order(r.providers) {
		resp, err := p.ChatStream(ctx, req)
		if err == nil {
			return resp, p.Name(), nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
	}
	return nil, "", fmt.Errorf("所有 Provider 均失败: %w", lastErr)
}
