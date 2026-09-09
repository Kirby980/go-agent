package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Kirby980/agent/llm"
)

// runReAct 实现经典 ReAct（Reasoning + Acting）推理决策循环：
// 1. 检查预算限制
// 2. 将 Observation: 作为 Stop 标志向模型发起思考请求（根据 stream 参数选择 ChatStream 或 Chat）
// 3. 解析模型输出（Thought, Action, Action Input, Final Answer）
// 4. 自愈机制：若模型未按规范输出，构造错误 Observation 反馈给模型让其纠错（最多重试 maxHealAttempts 次）
// 5. 若输出 Final Answer 则正常结束；若输出 Action 则调用相应工具，并将结果拼接为 Observation 追加到历史中继续循环
func (agent *Agent) runReAct(ctx context.Context, state *State, stream bool, emit func(AgentEvent) bool) {
	healAttempts := 0
	for {
		// --停止条件检查
		if stop, reason := agent.budget.Exceeded(state); stop {
			agent.finishError(ctx, state, emit, "提前终止"+reason)
			return
		}

		req := llm.ChatRequest{
			Model:    agent.model,
			Messages: state.Messages,
			Stop:     []string{"Observation:"},
		}

		var content string
		var streamedAnswerDeltas bool

		if stream && agent.provider.Capabilities().Streaming {
			streamCh, err := agent.provider.ChatStream(ctx, req)
			if err != nil {
				agent.finishError(ctx, state, emit, err.Error())
				return
			}

			var fullContent strings.Builder
			var emittedLen int

			for chunk := range streamCh {
				if chunk.Err != nil {
					agent.finishError(ctx, state, emit, chunk.Err.Error())
					return
				}
				fullContent.WriteString(chunk.Content)
				curr := fullContent.String()

				// 实时检测 Final Answer: 并逐字流式发出增量
				if idx := strings.Index(curr, "Final Answer:"); idx != -1 {
					tail := curr[idx+len("Final Answer:"):]
					if !streamedAnswerDeltas {
						trimmed := strings.TrimLeft(tail, " \t\r\n")
						if len(trimmed) > 0 {
							emit(AgentEvent{Type: EventAnswerDelta, Text: trimmed, Step: state.Step})
							emittedLen = len(tail)
							streamedAnswerDeltas = true
						}
					} else {
						if len(tail) > emittedLen {
							delta := tail[emittedLen:]
							emit(AgentEvent{Type: EventAnswerDelta, Text: delta, Step: state.Step})
							emittedLen = len(tail)
						}
					}
				}
			}
			content = fullContent.String()
		} else {
			// 非流式阶段：使用底层的 Chat 阻塞调用
			resp, err := agent.provider.Chat(ctx, req)
			if err != nil {
				agent.finishError(ctx, state, emit, err.Error())
				return
			}
			state.Usage.InputTokens += resp.InputTokens
			state.Usage.OutputTokens += resp.OutputTokens
			content = resp.Content
		}

		state.Step++
		state.UpdatedAt = time.Now()

		// --解析阶段
		step, err := parseReact(content)
		if err != nil {
			healAttempts++
			if healAttempts > agent.maxHealAttempts() {
				agent.finishError(ctx, state, emit, err.Error())
				return
			}
			if strings.TrimSpace(content) != "" {
				state.Messages = append(state.Messages, llm.Message{Role: llm.RoleAssistant, Content: content})
			}
			state.Messages = append(state.Messages, llm.Message{Role: llm.RoleUser, Content: "Observation: 错误：" + err.Error()})
			agent.checkpoint(ctx, state)
			continue
		}
		healAttempts = 0

		// --模型输出答案
		if step.FinalAnswer != "" {
			state.Phase = PhaseDone
			state.Answer = step.FinalAnswer
			state.Messages = append(state.Messages, llm.Message{Role: llm.RoleAssistant, Content: content})
			agent.checkpoint(ctx, state)
			// 如果是非流式模式，或者流式中未能提前检测到 Final Answer 前缀输出增量，在此处补发完整答案增量
			if !streamedAnswerDeltas {
				emit(AgentEvent{Type: EventAnswerDelta, Text: step.FinalAnswer, Step: state.Step})
			}
			emit(AgentEvent{Type: EventDone, Step: state.Step})
			return
		}

		if step.Thought != "" {
			emit(AgentEvent{Type: EventThought, Text: step.Thought, Step: state.Step})
		}

		// —— Acting 阶段：执行工具 ——
		rawArgs := json.RawMessage(step.ActionInput)
		if !agent.beforeToolCall(ctx, state, step.Action, rawArgs, emit) {
			return
		}
		observation := agent.callTool(ctx, step.Action, rawArgs)
		fmt.Println(step.Action, rawArgs)
		emit(AgentEvent{Type: EventToolResult, Tool: step.Action, Text: observation, Step: state.Step})

		// 把这一轮（模型的思考 + 工具的观察）追加进历史，回到下一轮 Thinking
		state.Messages = append(
			state.Messages,
			llm.Message{Role: llm.RoleAssistant, Content: content},
			llm.Message{Role: llm.RoleUser, Content: "Observation: " + observation},
		)
		agent.checkpoint(ctx, state)

	}
}

// finishError 把状态置为终态 error，落盘，并发出 error + done 两个事件。
func (agent *Agent) finishError(ctx context.Context, state *State, emit func(AgentEvent) bool, msg string) {
	state.Phase = PhaseError
	state.Answer = ""
	state.UpdatedAt = time.Now()
	agent.checkpoint(ctx, state)
	emit(AgentEvent{Type: EventError, Text: msg, Step: state.Step})
	emit(AgentEvent{Type: EventDone, Step: state.Step})
}

// callTool 执行工具，返回喂回模型的观察文本。
// 工具错误不中断循环，转成 Observation 让模型自己纠正。
func (agent *Agent) callTool(ctx context.Context, name string, args json.RawMessage) string {
	t, ok := agent.tools.Get(name)
	if !ok {
		return fmt.Sprintf("错误：未知工具 %q，可用工具：%v", name, agent.tools.ToolDefs())
	}
	out, err := t.Call(ctx, args)
	if err != nil {
		return "错误：" + err.Error()
	}
	return out
}

// checkpoint 清理历史无效消息后，将状态保存到内存 agent.memory，
// 并在配置了持久化 store 与 sessionID 时异步落盘。
func (agent *Agent) checkpoint(ctx context.Context, state *State) {
	state.Messages = dropEmptyAssistantMessages(state.Messages)
	agent.memory = state
	if agent.store == nil || agent.sessionID == "" {
		return
	}
	_ = agent.store.Save(ctx, agent.sessionID, state)
}

func dropEmptyAssistantMessages(messages []llm.Message) []llm.Message {
	if len(messages) == 0 {
		return messages
	}
	out := messages[:0]
	for _, message := range messages {
		if message.Role == llm.RoleAssistant &&
			strings.TrimSpace(message.Content) == "" &&
			len(message.ToolCalls) == 0 {
			continue
		}
		out = append(out, message)
	}
	return out
}

const reactSystemTemplate = `你是一个会使用工具完成任务的助手。

你可以使用以下工具：
%s

每一轮必须严格使用以下格式之一：

Thought: 你的简短思考
Action: 要使用的工具名称
Action Input: 调用工具的 JSON 参数

或者在任务完成时输出：

Final Answer: 给用户的最终答案

工具执行结果会由程序作为 Observation 返回。`

type reactStep struct {
	Thought     string // 模型的简短思考
	Action      string // 要调用的工具
	ActionInput string // 工具参数 JSON 文本
	FinalAnswer string // 若非空，表示循环结束
}

// parseReact 解析模型基于 ReAct 协议输出的思考与动作。
func parseReact(text string) (reactStep, error) {
	var step reactStep
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	for _, line := range strings.Split(normalized, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Final Answer:"):
			step.FinalAnswer = strings.TrimSpace(strings.TrimPrefix(line, "Final Answer:"))
		case strings.HasPrefix(line, "Thought:"):
			step.Thought = strings.TrimSpace(strings.TrimPrefix(line, "Thought:"))
		case strings.HasPrefix(line, "Action:"):
			step.Action = strings.TrimSpace(strings.TrimPrefix(line, "Action:"))
		case strings.HasPrefix(line, "Action Input:"):
			step.ActionInput = strings.TrimSpace(strings.TrimPrefix(line, "Action Input:"))
		}
	}
	if step.FinalAnswer != "" {
		return step, nil
	}
	if step.Action == "" || step.ActionInput == "" {
		return step, fmt.Errorf("ReAct 解析失败：未找到 Action / Action Input")
	}
	return step, nil
}
