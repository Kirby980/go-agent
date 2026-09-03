package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"test/agent/internal/llm"
	"test/agent/internal/tool"
)

const defaultSystemPrompt = "你是一个命令行 AI 助手。需要真实计算或查询当前时间时，请调用工具。"

type Agent struct {
	provider     llm.Provider   // 模型出口；传入 M02 的单个 Provider 或 router 适配器
	model        string         // 模型名
	tools        *tool.Registry // 可用工具集合
	systemPrompt string         // 系统提示词
	budget       Budget         // 停止条件，见 4.6
	store        Store          // 状态持久化，可选，见 4.10
	sessionID    string         // 会话 ID，配合 store 使用
	memory       *State         // 无持久化存储时的进程内会话状态
}

func New(provider llm.Provider, model string, registry *tool.Registry, opts ...Option) *Agent {
	agent := &Agent{
		provider:     provider,
		model:        model,
		tools:        registry,
		systemPrompt: defaultSystemPrompt,
		budget:       DefaultBudget(), // 给个安全默认值
	}
	for _, opt := range opts {
		opt(agent)
	}
	return agent
}

// Option 用函数式选项配置可选项（沿用 M02 的模式）。
type Option func(*Agent)

func WithSystemPrompt(prompt string) Option {
	return func(agent *Agent) {
		agent.systemPrompt = prompt
	}
}

func WithBudget(budget Budget) Option {
	return func(agent *Agent) {
		agent.budget = budget
	}
}

func WithStore(store Store, sessionID string) Option {
	return func(agent *Agent) {
		agent.store = store
		agent.sessionID = strings.TrimSpace(sessionID)
	}
}

func actionSignature(name string, args []byte) string {
	return name + ":" + strings.TrimSpace(string(args))
}

func (agent *Agent) beforeToolCall(
	ctx context.Context,
	state *State,
	name string,
	args json.RawMessage,
	emit func(AgentEvent) bool,
) bool {
	if err := ctx.Err(); err != nil {
		agent.finishError(ctx, state, emit, err.Error())
		return false
	}
	signature := actionSignature(name, args)
	if state.ActionCounts == nil {
		state.ActionCounts = make(map[string]int)
	}
	state.ActionCounts[signature]++
	if agent.budget.MaxSameAction > 0 && state.ActionCounts[signature] > agent.budget.MaxSameAction {
		agent.finishError(ctx, state, emit, fmt.Sprintf("重复动作过多：%s", signature))
		return false
	}
	emit(AgentEvent{Type: EventToolCall, Tool: name, Args: string(args), Step: state.Step})
	return true
}

func (agent *Agent) maxHealAttempts() int {
	if agent.budget.MaxHealAttempts <= 0 {
		return 3
	}
	return agent.budget.MaxHealAttempts
}

func (agent *Agent) RunStream(ctx context.Context, goal string) <-chan AgentEvent {
	out := make(chan AgentEvent, 16)
	go func() {
		defer close(out) // M01 纪律：生产者负责关闭
		agent.run(ctx, goal, out)
	}()
	return out
}

func (agent *Agent) run(ctx context.Context, goal string, out chan<- AgentEvent) {
	emit := func(event AgentEvent) bool {
		select {
		case <-ctx.Done():
			return false
		case out <- event:
			return true
		}
	}

	if agent.provider == nil {
		emit(AgentEvent{Type: EventError, Text: "provider 不能为空"})
		return
	}
	if agent.tools == nil {
		agent.tools = tool.NewRegistry()
	}
	state, err := agent.initialState(ctx, goal)
	if err != nil {
		emit(AgentEvent{Type: EventError, Text: err.Error()})
		return
	}
	if agent.provider.Capabilities().Tools {
		agent.runFunctionCalling(ctx, state, emit)
		return
	}
	agent.runReAct(ctx, state, emit)
}
