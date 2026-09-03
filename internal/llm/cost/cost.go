package cost

import "sync"

type Pricing struct {
	InputPer1M  float64
	OutputPer1M float64
}

type Usage struct {
	InputTokens  int
	OutputTokens int
}

func (u *Usage) Cost(p Pricing) float64 {
	return float64(u.InputTokens)/1e6*p.InputPer1M +
		float64(u.OutputTokens)/1e6*p.OutputPer1M
}

// Accumulator 并发安全地累计一轮会话的用量与成本。
type Accumulator struct {
	mu    sync.Mutex
	Usage Usage
	Cost  float64
}

func (a *Accumulator) Add(u Usage, p Pricing) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Usage.InputTokens += u.InputTokens
	a.Usage.OutputTokens += u.OutputTokens
	a.Cost += u.Cost(p)
}
