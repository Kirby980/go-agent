package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Kirby980/agent/ctxeng"
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
	var compactedThisTurn bool
	var turnUsage llm.Usage
	for {
		if stop, reason := agent.budget.Exceeded(state); stop {
			agent.finishError(ctx, state, emit, "提前终止："+reason, &turnUsage)
			return
		}

		// 上下文预算门控治理：单轮执行内最多触发一次压缩，防止多工具调用循环中过度压缩导致上下文丢失
		if agent.ctxBudget != nil && agent.ctxBudget.History > 0 && !compactedThisTurn {
			historyText := ctxeng.JoinContent(state.Messages)
			if agent.ctxBudget.IsHistoryOver(historyText) {
				beforeTok := ctxeng.EstimateTokens(historyText)
				compacted, err := ctxeng.Compact(ctx, state.Messages, agent.keepRecent, agent.summarize)
				if err == nil && len(compacted) < len(state.Messages) {
					compactedThisTurn = true
					afterTok := ctxeng.EstimateTokens(ctxeng.JoinContent(compacted))
					emit(AgentEvent{
						Type: EventThought,
						Text: fmt.Sprintf("[上下文治理] 历史消息超预算 (%d tok > %d)，已触发 Compact 压缩至 %d tok (%d 条消息)",
							beforeTok, agent.ctxBudget.History, afterTok, len(compacted)),
						Step: state.Step,
					})
					state.Messages = compacted
				}
			}
		}

		req := llm.ChatRequest{
			Model:    agent.model,
			Messages: state.Messages,
			Tools:    agent.toolDefs(state),
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

			var lastUsage *llm.Usage
			for chunk := range streamCh {
				if chunk.Err != nil {
					agent.finishError(ctx, state, emit, chunk.Err.Error())
					return
				}
				if chunk.Usage != nil {
					lastUsage = chunk.Usage
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
			if lastUsage != nil {
				turnUsage.InputTokens += lastUsage.InputTokens
				turnUsage.OutputTokens += lastUsage.OutputTokens
				turnUsage.CachedTokens += lastUsage.CachedTokens

				state.Usage.InputTokens += lastUsage.InputTokens
				state.Usage.OutputTokens += lastUsage.OutputTokens
				state.Usage.CachedTokens += lastUsage.CachedTokens
				emit(AgentEvent{Type: EventUsage, Usage: lastUsage, Step: state.Step})
			}
			content = fullContent.String()
		} else {
			resp, err := agent.provider.Chat(ctx, req)
			if err != nil {
				agent.finishError(ctx, state, emit, err.Error(), &turnUsage)
				return
			}
			u := &llm.Usage{
				InputTokens:  resp.InputTokens,
				OutputTokens: resp.OutputTokens,
				CachedTokens: resp.CachedTokens,
			}
			turnUsage.InputTokens += u.InputTokens
			turnUsage.OutputTokens += u.OutputTokens
			turnUsage.CachedTokens += u.CachedTokens

			state.Usage.InputTokens += u.InputTokens
			state.Usage.OutputTokens += u.OutputTokens
			state.Usage.CachedTokens += u.CachedTokens
			emit(AgentEvent{Type: EventUsage, Usage: u, Step: state.Step})
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
				agent.finishError(ctx, state, emit, "模型返回空响应：没有回答内容，也没有工具调用", &turnUsage)
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
			emit(AgentEvent{
				Type:       EventDone,
				Step:       state.Step,
				Usage:      &turnUsage,
				TotalUsage: &state.Usage,
			})
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

// toolDefs 把注册表里的工具转成给模型的定义清单，支持按当前意图动态裁剪。
func (agent *Agent) toolDefs(state *State) []llm.ToolDef {
	if agent.tools == nil {
		return nil
	}
	all := agent.tools.All()
	if agent.maxSelectTools > 0 && len(all) > agent.maxSelectTools && state != nil {
		query := state.Goal
		for i := len(state.Messages) - 1; i >= 0; i-- {
			if state.Messages[i].Role == llm.RoleUser {
				query = state.Messages[i].Content
				break
			}
		}
		selected := ctxeng.SelectTools(query, all, agent.maxSelectTools)
		defs := make([]llm.ToolDef, 0, len(selected))
		for _, t := range selected {
			defs = append(defs, llm.ToolDef{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Parameters(),
			})
		}
		return defs
	}
	return agent.tools.ToolDefs()
}
