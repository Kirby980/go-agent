package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kirby980/agent/tool"
)

// bridgedTool 把一个 MCP 工具包装成 tool.Tool。
type bridgedTool struct {
	client *StdioClient
	def    MCPTool
	params json.RawMessage
}

func (t *bridgedTool) Name() string        { return t.def.Name }
func (t *bridgedTool) Description() string { return t.def.Description }
func (t *bridgedTool) Parameters() json.RawMessage {
	return t.params
}
func (t *bridgedTool) Call(ctx context.Context, args json.RawMessage) (string, error) {
	result, err := t.client.CallTool(ctx, t.def.Name, args)
	if err != nil {
		return "", err
	}
	text := result.Text()
	if result.IsError {
		return text, fmt.Errorf("MCP 工具 %q 返回错误", t.def.Name)
	}
	return text, nil
}

// BridgeAll 列出某个 MCP Server 的全部工具，桥接成 []tool.Tool。
func BridgeAll(ctx context.Context, client *StdioClient) ([]tool.Tool, error) {
	mcpTools, err := client.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]tool.Tool, 0, len(mcpTools))
	for _, mt := range mcpTools {
		params := mt.InputSchema
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, &bridgedTool{client: client, def: mt, params: params})
	}
	return out, nil
}
