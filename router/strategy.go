// Package router 提供了多大模型供应商（LLM Provider）的优先级路由与自动故障转移容灾机制。
package router

import (
	"sync/atomic"

	"github.com/Kirby980/agent/llm"
)

// Strategy 定义多 Provider 的动态排序决策策略。
type Strategy interface {
	// Order 根据当前策略对传入的 providers 进行优先级排序并返回。
	Order(providers []llm.Provider) []llm.Provider
}

// Priority 是基于静态优先级的路由策略：严格按注册顺序调用，主用第一个，失败时自动降级到下一个。
type Priority struct{}

// Order 返回原切片顺序。
func (Priority) Order(ps []llm.Provider) []llm.Provider { return ps }

// RoundRobin 实现并发安全的轮询负载均衡调度策略。
// 每次请求轮流选择下一个 Provider 作为首选主用节点；当首选节点调用失败时，自动按序降级尝试剩余节点。
type RoundRobin struct {
	counter uint64
}

// NewRoundRobin 创建一个轮询策略实例。
func NewRoundRobin() *RoundRobin {
	return &RoundRobin{}
}

// Order 将 providers 进行环形滚动偏移，实现公平轮询分发。
func (r *RoundRobin) Order(ps []llm.Provider) []llm.Provider {
	n := len(ps)
	if n <= 1 {
		return ps
	}
	start := int((atomic.AddUint64(&r.counter, 1) - 1) % uint64(n))
	res := make([]llm.Provider, n)
	for i := 0; i < n; i++ {
		res[i] = ps[(start+i)%n]
	}
	return res
}


