package router

import (
	"context"
	"fmt"

	"github.com/Kirby980/agent/llm"
)

// Router 实现多 LLM 供应商的智能路由与自动降级容灾。
type Router struct {
	providers []llm.Provider
	strategy  Strategy
}

// New 创建一个新的模型路由器，要求传入排序策略及至少一个有效的 Provider。
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

// Chat 按策略顺序依次尝试调用各个 Provider，遇到错误自动降级到下一个；
// 成功时返回响应、实际提供响应的 Provider 名称，若全部失败则返回错误。
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

// ChatStream 按策略顺序发起流式请求，首个成功的 Provider 返回其输出 channel 及 Provider 名称；
// 若某个 Provider 失败，则在未开始产生数据前尝试下一个，全部失败则返回错误。
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
