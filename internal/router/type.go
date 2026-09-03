package router

import "test/agent/internal/llm"

type Strategy interface {
	Order(providers []llm.Provider) []llm.Provider
}

type Priority struct{} // 最简策略：按注册顺序，主用第一个，挂了降级

func (Priority) Order(ps []llm.Provider) []llm.Provider { return ps }

// type CheapestFirst struct{
// }

// func (CheapestFirst)Order(ps []llm.Provider)[]llm.Provider{

// }
