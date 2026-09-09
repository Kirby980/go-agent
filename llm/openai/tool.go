package openai

import (
	"encoding/json"

	"github.com/Kirby980/agent/llm"
)

type openaiTool struct {
	Type     string            `json:"type"` // 固定 function
	Function openaiFunctionDef `json:"function"`
}

type openaiFunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

func toOpenAITools(defs []llm.ToolDef) []openaiTool {
	if len(defs) == 0 {
		return nil
	}
	out := make([]openaiTool, len(defs))
	for i, d := range defs {
		out[i] = openaiTool{
			Type:     "function",
			Function: openaiFunctionDef{Name: d.Name, Description: d.Description, Parameters: d.Parameters},
		}
	}
	return out
}

// 响应中工具调用的形状
type respToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // 注意：这是 JSON 字符串，不是对象
	} `json:"function"`
}

// 把 OpenAI 的 tool_calls 解析回我们中立的 llm.ToolCall。
func parseToolCalls(raw []respToolCall) []llm.ToolCall {
	if len(raw) == 0 {
		return nil
	}
	out := make([]llm.ToolCall, len(raw))
	for i, tc := range raw {
		out[i] = llm.ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Args: json.RawMessage(tc.Function.Arguments),
		}
	}
	return out
}

type chatMsg struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []respToolCall `json:"tool_calls,omitempty"`   // assistant 发起的调用
	ToolCallID string         `json:"tool_call_id,omitempty"` // tool 结果对应的调用 id
}

func toOpenAIMessages(msgs []llm.Message) []chatMsg {
	out := make([]chatMsg, len(msgs))
	for i, m := range msgs {
		cm := chatMsg{Role: string(m.Role), Content: m.Content, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			var rtc respToolCall
			rtc.ID = tc.ID
			rtc.Type = "function"
			rtc.Function.Name = tc.Name
			rtc.Function.Arguments = string(tc.Args)
			cm.ToolCalls = append(cm.ToolCalls, rtc)
		}
		out[i] = cm
	}
	return out
}
