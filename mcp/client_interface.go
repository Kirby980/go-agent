package mcp

import (
	"context"
	"encoding/json"
)

type Client interface {
	Initialize(ctx context.Context) (ServerInfo, error)
	Initialized(ctx context.Context) error
	ListTools(ctx context.Context) ([]MCPTool, error)
	CallTool(ctx context.Context, name string, args json.RawMessage) (CallToolResult, error)
	Close() error
}
