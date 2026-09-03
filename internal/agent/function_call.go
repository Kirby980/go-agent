package agent

import (
	"context"
	"strings"
	"test/agent/internal/llm"
	"time"
)

func (agent *Agent) runFunctionCalling(
	ctx context.Context,
	state *State,
	emit func(AgentEvent) bool,
) {
	for {
		if stop, reason := agent.budget.Exceeded(state); stop {
			agent.finishError(ctx, state, emit, "提前终止："+reason)
			return
		}

		resp, err := agent.provider.Chat(ctx, llm.ChatRequest{
			Model:    agent.model,
			Messages: state.Messages,
			Tools:    agent.toolDefs(),
		})
		if err != nil {
			agent.finishError(ctx, state, emit, err.Error())
			return
		}
		state.Step++
		state.Usage.InputTokens += resp.InputTokens
		state.Usage.OutputTokens += resp.OutputTokens
		state.UpdatedAt = time.Now()

		// 模型没有要调用任何工具 → 这就是最终答案
		if len(resp.ToolCalls) == 0 {
			answer := strings.TrimSpace(resp.Content)
			if answer == "" {
				agent.finishError(ctx, state, emit, "模型返回空响应：没有回答内容，也没有工具调用")
				return
			}
			state.Phase = PhaseDone
			state.Answer = answer
			state.Messages = append(state.Messages, llm.Message{
				Role:    llm.RoleAssistant,
				Content: answer,
			})
			agent.checkpoint(ctx, state)
			emit(AgentEvent{Type: EventAnswerDelta, Text: answer, Step: state.Step})
			emit(AgentEvent{Type: EventDone, Step: state.Step})
			return
		}

		state.Phase = PhaseActing
		if strings.TrimSpace(resp.Content) != "" {
			emit(AgentEvent{Type: EventThought, Text: resp.Content, Step: state.Step})
		}
		state.Messages = append(state.Messages, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// 逐个执行工具，每个结果作为一条 tool 消息回填（注意要带 ToolCallID）
		for _, call := range resp.ToolCalls {
			if !agent.beforeToolCall(ctx, state, call.Name, call.Args, emit) {
				return
			}
			observation := agent.callTool(ctx, call.Name, call.Args)
			emit(AgentEvent{Type: EventToolResult, Tool: call.Name, Text: observation, Step: state.Step})
			state.Messages = append(state.Messages, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: call.ID,
				Content:    observation,
			})
		}
		state.Phase = PhaseThinking
		agent.checkpoint(ctx, state)
	}
}

// toolDefs 把注册表里的工具转成给模型的定义清单。
func (agent *Agent) toolDefs() []llm.ToolDef {
	if agent.tools == nil {
		return nil
	}
	return agent.tools.ToolDefs()
}
