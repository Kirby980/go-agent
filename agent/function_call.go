package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Kirby980/agent/llm"
)

// runFunctionCalling 实现基于模型原生 Function Calling 能力的自主运行循环：
// 1. 每轮前检查预算限制（步数、token上限、截止时间等）
// 2. 向模型发起带有当前可用工具列表 Tools 的聊天请求（根据 stream 参数选择 ChatStream 或 Chat）
// 3. 若模型未发起 ToolCalls，则判定为最终答案，状态置为 PhaseDone，发出增量及完成事件并持久化
// 4. 若模型发起了 ToolCalls，则遍历调用工具，产生 Observation 结果，以 RoleTool 消息携带对应 ToolCallID 回填
// 5. 循环回到第 1 步，直到完成或超出预算
func (agent *Agent) runFunctionCalling(
	ctx context.Context,
	state *State,
	stream bool,
	emit func(AgentEvent) bool,
) {
	for {
		if stop, reason := agent.budget.Exceeded(state); stop {
			agent.finishError(ctx, state, emit, "提前终止："+reason)
			return
		}

		req := llm.ChatRequest{
			Model:    agent.model,
			Messages: state.Messages,
			Tools:    agent.toolDefs(),
		}

		var content string
		var toolCalls []llm.ToolCall
		var streamedAnswerDeltas bool

		if stream && agent.provider.Capabilities().Streaming {
			streamCh, err := agent.provider.ChatStream(ctx, req)
			if err != nil {
				agent.finishError(ctx, state, emit, err.Error())
				return
			}

			var fullContent strings.Builder

			for chunk := range streamCh {
				if chunk.Err != nil {
					agent.finishError(ctx, state, emit, chunk.Err.Error())
					return
				}
				if chunk.Content != "" {
					fullContent.WriteString(chunk.Content)
					if !strings.HasPrefix(strings.TrimSpace(fullContent.String()), "Thought:") {
						emit(AgentEvent{Type: EventAnswerDelta, Text: chunk.Content, Step: state.Step})
						streamedAnswerDeltas = true
					}
				}
				if len(chunk.ToolCalls) > 0 {
					toolCalls = append(toolCalls, chunk.ToolCalls...)
				}
			}
			content = fullContent.String()
		} else {
			resp, err := agent.provider.Chat(ctx, req)
			if err != nil {
				agent.finishError(ctx, state, emit, err.Error())
				return
			}
			state.Usage.InputTokens += resp.InputTokens
			state.Usage.OutputTokens += resp.OutputTokens
			content = resp.Content
			toolCalls = resp.ToolCalls
		}

		state.Step++
		state.UpdatedAt = time.Now()

		// 检查是否通过文本降级输出了 ReAct 格式动作（Thought/Action/Action Input）
		if len(toolCalls) == 0 {
			if step, err := parseReact(content); err == nil && step.Action != "" {
				toolCalls = []llm.ToolCall{
					{
						ID:   fmt.Sprintf("call_react_%d", state.Step),
						Name: step.Action,
						Args: json.RawMessage(step.ActionInput),
					},
				}
				if step.Thought != "" {
					content = step.Thought
				}
			}
		}

		// 模型没有要调用任何工具 → 这就是最终答案
		if len(toolCalls) == 0 {
			answer := strings.TrimSpace(content)
			if step, err := parseReact(content); err == nil && step.FinalAnswer != "" {
				answer = step.FinalAnswer
			}
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
			if !streamedAnswerDeltas {
				emit(AgentEvent{Type: EventAnswerDelta, Text: answer, Step: state.Step})
			}
			emit(AgentEvent{Type: EventDone, Step: state.Step})
			return
		}

		state.Phase = PhaseActing
		if strings.TrimSpace(content) != "" {
			emit(AgentEvent{Type: EventThought, Text: content, Step: state.Step})
		}
		state.Messages = append(state.Messages, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   content,
			ToolCalls: toolCalls,
		})

		// 逐个执行工具，每个结果作为一条 tool 消息回填（注意要带 ToolCallID）
		for _, call := range toolCalls {
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
