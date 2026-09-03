package agent

import (
	"test/agent/internal/llm"
	"time"
)

type Phase string

const (
	PhaseThinking Phase = "thinking"
	PhaseActing   Phase = "acting"
	PhaseDone     Phase = "done"
	PhaseError    Phase = "error"
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
