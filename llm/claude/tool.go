package claude

import (
	"encoding/json"

	"github.com/Kirby980/agent/llm"
)

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

func toAnthropicTool(defs []llm.ToolDef) []anthropicTool {
	out := make([]anthropicTool, len(defs))
	for i, def := range defs {
		out[i] = anthropicTool{
			Name:        def.Name,
			Description: def.Description,
			InputSchema: def.Parameters,
		}
	}
	return out
}

type contentBlock struct {
	Type      string          `json:"type"` // text / tool_call / tool_result
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

func parseAnthropicResponse(blocks []contentBlock) (text string, calls []llm.ToolCall) {
	for _, b := range blocks {
		switch b.Type {
		case "text":
			text += b.Text
		case "tool_use":
			calls = append(calls, llm.ToolCall{
				ID:   b.ID,
				Name: b.Name,
				Args: b.Input,
			})
		}
	}
	return
}

type anthropicMsg struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

func toAnthropicMessages(msgs []llm.Message) []anthropicMsg {
	var out []anthropicMsg
	for _, m := range msgs {
		switch m.Role {
		case llm.RoleTool:
			block := contentBlock{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   m.Content,
			}
			// Anthropic 强制要求 user/assistant 角色严格交替。
			// 当模型单轮并行发起多个工具调用时，多个 tool_result 必须合并在同一个 user message 内。
			if len(out) > 0 && out[len(out)-1].Role == "user" {
				out[len(out)-1].Content = append(out[len(out)-1].Content, block)
			} else {
				out = append(out, anthropicMsg{
					Role:    "user",
					Content: []contentBlock{block},
				})
			}
		case llm.RoleAssistant:
			var blocks []contentBlock
			if m.Content != "" {
				blocks = append(blocks, contentBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, contentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: tc.Args,
				})
			}
			out = append(out, anthropicMsg{Role: "assistant", Content: blocks})
		default:
			out = append(out, anthropicMsg{
				Role:    string(m.Role),
				Content: []contentBlock{{Type: "text", Text: m.Content}},
			})
		}
	}
	return out
}
