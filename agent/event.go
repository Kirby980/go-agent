package agent

// EventType 表示 Agent 在执行生命周期中产生的事件类型。
type EventType string

const (
	// EventThought 表示模型产生的一段思考推理过程（Thought）。
	EventThought EventType = "thought"
	// EventToolCall 表示即将发起某个具体工具的调用。
	EventToolCall EventType = "tool_call"
	// EventToolResult 表示工具执行完毕并返回了观察结果（Observation）。
	EventToolResult EventType = "tool_result"
	// EventAnswerDelta 表示最终回复文本的一个流式增量。
	EventAnswerDelta EventType = "answer_delta"
	// EventError 表示执行过程中发生了不可恢复的错误。
	EventError EventType = "error"
	// EventDone 表示当前任务执行结束（无论是成功还是失败终态）。
	EventDone EventType = "done"
)

// AgentEvent 是 Agent 运行过程中向外发出的一个事件。
type AgentEvent struct {
	Type EventType `json:"type"`
	Text string    `json:"text,omitempty"` // 思考内容 / 答案增量 / 错误信息
	Tool string    `json:"tool,omitempty"` // 涉及的工具名
	Args string    `json:"args,omitempty"` // 工具参数
	Step int       `json:"step,omitempty"` // 当前 Agent 步数
}
