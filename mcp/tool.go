package mcp

import (
	"context"
	"encoding/json"
	"strings"
)

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (c *StdioClient) Initialize(ctx context.Context) (ServerInfo, error) {
	params := map[string]any{
		"protocolVersion": "2025-11-25", // 建议做成可配置项
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "llmagent", "version": "0.1.0"},
	}
	raw, err := c.call(ctx, "initialize", params)
	if err != nil {
		return ServerInfo{}, err
	}
	var out struct {
		ServerInfo ServerInfo `json:"serverInfo"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return ServerInfo{}, err
	}
	return out.ServerInfo, nil
}

func (c *StdioClient) Initialized(ctx context.Context) error {
	return c.notify("notifications/initialized", nil)
}

type MCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

func (c *StdioClient) ListTools(ctx context.Context) ([]MCPTool, error) {
	raw, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Tools []MCPTool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

func (r CallToolResult) Text() string {
	var sb strings.Builder
	for _, b := range r.Content {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

func (c *StdioClient) CallTool(ctx context.Context, name string, args json.RawMessage) (CallToolResult, error) {
	params := map[string]any{"name": name, "arguments": json.RawMessage(args)}
	raw, err := c.call(ctx, "tools/call", params)
	if err != nil {
		return CallToolResult{}, err
	}
	var out CallToolResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return CallToolResult{}, err
	}
	return out, nil
}
