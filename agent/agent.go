// Package agent 提供了自主智能体（AI Agent）核心调度引擎，
// 支持原生 Function Calling 与经典 ReAct 双执行模式、状态记忆与持久化、预算控制及流式事件通知。
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Kirby980/agent/llm"
	"github.com/Kirby980/agent/tool"
)

const defaultSystemPrompt = "你是一个命令行 AI 助手。需要真实计算或查询当前时间时，请调用工具。"

// Agent 是自主智能体的调度核心，持有模型出口、可用工具集、系统提示词、预算策略及状态持久化组件。
type Agent struct {
	provider     llm.Provider   // 模型出口；可传入单个 Provider 或 router 多模型路由器
	model        string         // 调用的模型名称
	tools        *tool.Registry // 可用工具注册表
	systemPrompt string         // 系统提示词
	budget       Budget         // 运行停止条件与预算限制
	store        Store          // 状态持久化存储（可选）
	sessionID    string         // 会话 ID，配合 store 使用
	memory       *State         // 进程内缓存的最新会话状态
}

// New 创建并初始化一个 Agent 实例。
// 必须提供 provider（模型提供方）、model（模型名）和 registry（工具注册表），可选传入 With* 函数选项定制配置。
func New(provider llm.Provider, model string, registry *tool.Registry, opts ...Option) *Agent {
	agent := &Agent{
		provider:     provider,
		model:        model,
		tools:        registry,
		systemPrompt: defaultSystemPrompt,
		budget:       DefaultBudget(), // 默认提供安全防死循环的运行预算
	}
	for _, opt := range opts {
		opt(agent)
	}
	return agent
}

// Option 定义 Agent 的函数式配置选项。
type Option func(*Agent)

// WithSystemPrompt 设置自定义系统提示词。
func WithSystemPrompt(prompt string) Option {
	return func(agent *Agent) {
		agent.systemPrompt = prompt
	}
}

// WithBudget 设置运行预算（如最大步数、token 上限、单动作重复限制等）。
func WithBudget(budget Budget) Option {
	return func(agent *Agent) {
		agent.budget = budget
	}
}

// WithStore 设置状态持久化存储实现及关联的 sessionID。
func WithStore(store Store, sessionID string) Option {
	return func(agent *Agent) {
		agent.store = store
		agent.sessionID = strings.TrimSpace(sessionID)
	}
}

// actionSignature 生成动作唯一标识（工具名 + 参数文本），用于死循环与重复动作检测。
func actionSignature(name string, args []byte) string {
	return name + ":" + strings.TrimSpace(string(args))
}

// beforeToolCall 在执行工具前触发：
// 1. 检查 Context 是否已取消
// 2. 统计当前动作调用次数，超出 MaxSameAction 预算时终止，防止模型死循环
// 3. 产生并发送 EventToolCall 事件
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

// maxHealAttempts 获取 ReAct 格式解析失败后的最大重试修正次数。
func (agent *Agent) maxHealAttempts() int {
	if agent.budget.MaxHealAttempts <= 0 {
		return 3
	}
	return agent.budget.MaxHealAttempts
}

// RunStream 以流式方式启动 Agent 执行任务：
// 接收目标 goal，在后台 goroutine 中执行并返回一个只读的事件 channel，
// 遵循通道生产方负责关闭原则（生产者 defer close(out)）。
func (agent *Agent) RunStream(ctx context.Context, goal string) <-chan AgentEvent {
	out := make(chan AgentEvent, 16)
	go func() {
		defer close(out) // M01 纪律：生产者负责关闭
		agent.run(ctx, goal, out)
	}()
	return out
}

// run 调度 Agent 执行主循环：
// 1. 初始化并校验依赖（Provider、Tool Registry）
// 2. 加载历史状态或初始化新状态（initialState）
// 3. 根据底层 Provider 是否支持原生 Function Calling 决定分流：
//    - 支持 Tools: 进入 runFunctionCalling 原生工具调用循环
//    - 不支持: 降级进入 runReAct（Thought-Action-Observation 文本自愈循环）
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

// initialState 构造或恢复本次执行的状态快照：
// 1. 若配置了持久化 Store 和 SessionID，优先从存储加载；若会话文件不存在则新建
// 2. 若未配置 Store 则尝试沿用内存中的 agent.memory
// 3. 初始化新状态或更新现有状态的当前轮目标与阶段
// 4. 根据模式（Function Calling vs ReAct）补充系统提示词与工具清单说明
// 5. 将当前目标作为一条 User 消息追加至消息历史中
func (agent *Agent) initialState(ctx context.Context, goal string) (*State, error) {
	var state *State
	if agent.store != nil && agent.sessionID != "" {
		loaded, err := agent.store.Load(ctx, agent.sessionID)
		if err == nil {
			state = loaded
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	} else if agent.memory != nil {
		state = agent.memory
	}

	now := time.Now()
	if state == nil {
		state = &State{
			Goal:         goal,
			Phase:        PhaseThinking,
			ActionCounts: make(map[string]int),
			StartedAt:    now,
			UpdatedAt:    now,
		}
		if sysPrompt := agent.buildSystemPrompt(); sysPrompt != "" {
			state.Messages = append(state.Messages, llm.Message{
				Role:    llm.RoleSystem,
				Content: sysPrompt,
			})
		}
	} else {
		state.Goal = goal
		state.Phase = PhaseThinking
		if state.ActionCounts == nil {
			state.ActionCounts = make(map[string]int)
		}
		state.UpdatedAt = now
		if len(state.Messages) == 0 {
			if sysPrompt := agent.buildSystemPrompt(); sysPrompt != "" {
				state.Messages = append(state.Messages, llm.Message{
					Role:    llm.RoleSystem,
					Content: sysPrompt,
				})
			}
		}
	}

	if strings.TrimSpace(goal) != "" {
		state.Messages = append(state.Messages, llm.Message{
			Role:    llm.RoleUser,
			Content: goal,
		})
	}
	agent.memory = state
	return state, nil
}

// buildSystemPrompt 根据当前能力生成系统提示词：
// - 原生支持 Tools 时，使用通用的 systemPrompt（工具定义通过 API 请求入参传递）
// - 非原生 Tools 时（ReAct 模式），将 Registry 中注册的所有工具详情（名称、描述、参数 JSON Schema）
//   格式化填入 ReAct 格式模板中，告知模型可调用哪些工具及严格的 Thought/Action 交互协议。
func (agent *Agent) buildSystemPrompt() string {
	if agent.provider != nil && agent.provider.Capabilities().Tools {
		return agent.systemPrompt
	}
	var toolLines []string
	if agent.tools != nil {
		for _, t := range agent.tools.All() {
			if len(t.Parameters()) > 0 {
				toolLines = append(toolLines, fmt.Sprintf("- %s: %s, 参数: %s", t.Name(), t.Description(), string(t.Parameters())))
			} else {
				toolLines = append(toolLines, fmt.Sprintf("- %s: %s", t.Name(), t.Description()))
			}
		}
	}
	toolText := strings.Join(toolLines, "\n")
	if toolText == "" {
		toolText = "（无可用工具）"
	}
	prompt := fmt.Sprintf(reactSystemTemplate, toolText)
	if agent.systemPrompt != "" && agent.systemPrompt != defaultSystemPrompt {
		prompt = agent.systemPrompt + "\n\n" + prompt
	}
	return prompt
}
