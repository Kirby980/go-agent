package agent

type EventType string

const (
	EventThought     EventType = "thought"      // 模型的一段思考
	EventToolCall    EventType = "tool_call"    // 即将调用某工具
	EventToolResult  EventType = "tool_result"  // 工具返回了结果
	EventAnswerDelta EventType = "answer_delta" // 最终答案的一个增量（流式）
	EventError       EventType = "error"
	EventDone        EventType = "done"
)

// AgentEvent 是 Agent 运行过程中向外发出的一个事件。
type AgentEvent struct {
	Type EventType `json:"type"`
	Text string    `json:"text,omitempty"` // 思考内容 / 答案增量 / 错误信息
	Tool string    `json:"tool,omitempty"` // 涉及的工具名
	Args string    `json:"args,omitempty"` // 工具参数
	Step int       `json:"step,omitempty"` // 当前 Agent 步数
}
