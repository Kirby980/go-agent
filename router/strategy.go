// Package router 提供了多大模型供应商（LLM Provider）的优先级路由与自动故障转移容灾机制。
package router

import "github.com/Kirby980/agent/llm"

// Strategy 定义多 Provider 的动态排序决策策略。
type Strategy interface {
	// Order 根据当前策略对传入的 providers 进行优先级排序并返回。
	Order(providers []llm.Provider) []llm.Provider
}

// Priority 是基于静态优先级的路由策略：严格按注册顺序调用，主用第一个，失败时自动降级到下一个。
type Priority struct{}

// Order 返回原切片顺序。
func (Priority) Order(ps []llm.Provider) []llm.Provider { return ps }

// type CheapestFirst struct{
// }

// func (CheapestFirst)Order(ps []llm.Provider)[]llm.Provider{

// }
