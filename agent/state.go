package agent

import (
	"time"

	"github.com/Kirby980/agent/llm"
)

// Phase 表示 Agent 当前所处的执行阶段状态机。
type Phase string

const (
	// PhaseThinking 思考阶段：正在向模型请求推理决策。
	PhaseThinking Phase = "thinking"
	// PhaseActing 行动阶段：正在执行模型决定调用的外部工具。
	PhaseActing Phase = "acting"
	// PhaseDone 完成阶段：任务已顺利完成并产出最终回答。
	PhaseDone Phase = "done"
	// PhaseError 错误阶段：执行中发生不可恢复错误或超出预算提前终止。
	PhaseError Phase = "error"
)

// State 是一次 Agent 运行的完整快照，刻意设计成可 JSON 序列化（见 4.10）。
type State struct {
	Goal         string            `json:"goal"`     // 用户给的目标
	Messages     []llm.Message     `json:"messages"` // 完整对话历史，含工具结果——这是 Agent 的“记忆”
	Step         int               `json:"step"`     // 已执行的步数
	Phase        Phase             `json:"phase"`
	Answer       string            `json:"answer,omitempty"`        // 终态时的最终答案
	Usage        llm.Usage         `json:"usage"`                   // 累计 token 用量
	ActionCounts map[string]int    `json:"action_counts,omitempty"` // 重复动作检测，见 4.6
	StartedAt    time.Time         `json:"started_at"`              // 本轮开始时间
	UpdatedAt    time.Time         `json:"updated_at"`              // 最近一次状态更新时间
	Metadata     map[string]string `json:"metadata,omitempty"`      // 预留给业务侧扩展
}

func (s *State) tokenTotal() int {
	return s.Usage.InputTokens + s.Usage.OutputTokens
}
