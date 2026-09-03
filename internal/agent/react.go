package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"test/agent/internal/llm"
	"time"
)

func (agent *Agent) runReAct(ctx context.Context, state *State, emit func(AgentEvent) bool) {
	healAttempts := 0
	for {
		// --停止条件检查
		if stop, reason := agent.budget.Exceeded(state); stop {
			agent.finishError(ctx, state, emit, "提前终止"+reason)
			return
		}

		// --思考阶段
		resp, err := agent.provider.Chat(ctx, llm.ChatRequest{
			Model:    agent.model,
			Messages: state.Messages,
			Stop:     []string{"Observation:"},
		})
		if err != nil {
			agent.finishError(ctx, state, emit, err.Error())
			return
		}

		state.Step++
		state.Usage.InputTokens += resp.InputTokens
		state.Usage.OutputTokens += resp.OutputTokens
		state.UpdatedAt = time.Now()

		// --解析阶段
		step, err := parseReact(resp.Content)
		if err != nil {
			healAttempts++
			if healAttempts > agent.maxHealAttempts() {
				agent.finishError(ctx, state, emit, err.Error())
				return
			}
			if strings.TrimSpace(resp.Content) != "" {
				state.Messages = append(state.Messages, llm.Message{Role: llm.RoleAssistant, Content: resp.Content})
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
			state.Messages = append(state.Messages, llm.Message{Role: llm.RoleAssistant, Content: resp.Content})
			agent.checkpoint(ctx, state)
			emit(AgentEvent{Type: EventAnswerDelta, Text: step.FinalAnswer, Step: state.Step})
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
		emit(AgentEvent{Type: EventToolResult, Tool: step.Action, Text: observation, Step: state.Step})

		// 把这一轮（模型的思考 + 工具的观察）追加进历史，回到下一轮 Thinking
		state.Messages = append(
			state.Messages,
			llm.Message{Role: llm.RoleAssistant, Content: resp.Content},
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
