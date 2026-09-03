package llm

import "sync"

// Pricing 定义模型的价格标准（每百万 Token 的价格）。
type Pricing struct {
	InputPer1M  float64 // 每百万输入 Token 费用
	OutputPer1M float64 // 每百万输出 Token 费用
}

// Usage 记录模型调用的 Token 消耗量。
type Usage struct {
	InputTokens  int `json:"input_tokens"`  // 输入 Prompt Token 数量
	OutputTokens int `json:"output_tokens"` // 输出 Completion Token 数量
}

// Cost 根据指定的价格计费标准计算当前用量的实际花费金额。
func (u *Usage) Cost(p Pricing) float64 {
	return float64(u.InputTokens)/1e6*p.InputPer1M +
		float64(u.OutputTokens)/1e6*p.OutputPer1M
}

// Accumulator 是并发安全的 Token 用量与费用累加器。
type Accumulator struct {
	mu    sync.Mutex
	Usage Usage
	Cost  float64
}

// Add 累加单次请求的用量与费用。
func (a *Accumulator) Add(u Usage, p Pricing) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Usage.InputTokens += u.InputTokens
	a.Usage.OutputTokens += u.OutputTokens
	a.Cost += u.Cost(p)
}
