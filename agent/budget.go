package agent

import (
	"fmt"
	"time"
)

// Budget 定义 Agent 运行期间的资源与安全限制，防止由于模型幻觉或逻辑错误引发死循环与无限费用消耗。
type Budget struct {
	MaxSteps        int       // 最大推理步数（默认 10）
	MaxTokens       int       // 累计 token 上限，0 表示不限
	Deadline        time.Time // 截止时刻，零值表示不限
	MaxSameAction   int       // 同一动作签名允许连续/累计调用的最大次数，用于死循环检测
	MaxHealAttempts int       // ReAct 文本格式解析失败后的最大自愈重试次数
}

// DefaultBudget 给 Agent 一个安全默认值，CLI 可用参数覆盖其中部分字段。
func DefaultBudget() Budget {
	return Budget{
		MaxSteps:        10,
		MaxTokens:       12000,
		MaxSameAction:   3,
		MaxHealAttempts: 3,
	}
}

// Exceeded 判断当前状态是否已触发任一停止条件。
func (budget Budget) Exceeded(state *State) (bool, string) {
	if state == nil {
		return false, ""
	}
	if budget.MaxSteps > 0 && state.Step >= budget.MaxSteps {
		return true, fmt.Sprintf("达到最大步骤数 %d", budget.MaxSteps)
	}
	if budget.MaxTokens > 0 && state.tokenTotal() >= budget.MaxTokens {
		return true, fmt.Sprintf("达到 token 预算 %d", budget.MaxTokens)
	}
	if !budget.Deadline.IsZero() && time.Now().After(budget.Deadline) {
		return true, "达到运行截止时间"
	}
	return false, ""
}
