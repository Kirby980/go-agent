package ctxeng

// Budget 描述一次模型调用里各部分的 token 上限（0 表示不限）。
type Budget struct {
	Total        int
	SystemPrompt int
	Tools        int
	History      int
	Retrieved    int
}

func (b *Budget) Over(systemPrompt, tools, history, retrieved string) map[string]int {
	over := map[string]int{}
	chk := func(name, text string, limit int) {
		if limit > 0 {
			if n := EstimateTokens(text); n > limit {
				over[name] = n - limit
			}
		}
	}
	chk("system", systemPrompt, b.SystemPrompt)
	chk("tools", tools, b.Tools)
	chk("history", history, b.History)
	chk("retrieved", retrieved, b.Retrieved)
	return over
}

// IsHistoryOver 检查历史文本是否超出预算门控
func (b *Budget) IsHistoryOver(historyText string) bool {
	if b == nil || b.History <= 0 {
		return false
	}
	return EstimateTokens(historyText) > b.History
}

