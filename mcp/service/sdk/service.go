package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Kirby980/agent/tool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	s := newServer()
	// 使用 stdio 传输运行 server（标准输入输出与客户端通信）
	if err := s.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func newServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "mcp-go demo",
		Version: "0.1.0",
	}, nil)

	// 注意：必须使用包级泛型函数 mcp.AddTool，而不是 s.AddTool
	mcp.AddTool(s, newGetTimeTool(), handleGetTime)
	mcp.AddTool(s, newCalcTool(), handleCalc)
	return s
}

func newGetTimeTool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "get_time",
		Description: "返回当前时间，支持按 IANA 时区格式化",
	}
}

// Input 结构体会被 SDK 自动用于生成 InputSchema 并自动反序列化
type Input struct {
	TimeZone string `json:"timezone,omitempty" jsonschema:"时区，IANA 时区格式，如 Asia/Shanghai"`
}


// handleGetTime 的签名满足 mcp.ToolHandlerFor[Input, any]
func handleGetTime(_ context.Context, _ *mcp.CallToolRequest, input Input) (*mcp.CallToolResult, any, error) {
	timezone := input.TimeZone
	loc := time.Local
	if timezone != "" {
		loaded, err := time.LoadLocation(timezone)
		if err != nil {
			// 在 ToolHandlerFor 中，直接返回普通 error，SDK 会自动将其作为 Tool 错误返回（IsError: true，错误信息填入 Content）
			return nil, nil, fmt.Errorf("无效时区 %q: %w", timezone, err)
		}
		loc = loaded
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: time.Now().In(loc).Format(time.RFC3339),
			},
		},
	}, nil, nil
}

func newCalcTool() *mcp.Tool {
	return &mcp.Tool{
		Name:        "calc",
		Description: "计算只包含数字、括号、+、-、*、/ 的算术表达式",
	}
}

type calcInput struct {
	Expr string `json:"expr" jsonschema:"四则运算表达式，例如 1+2*3"`
}


func handleCalc(_ context.Context, _ *mcp.CallToolRequest, input calcInput) (*mcp.CallToolResult, any, error) {
	if input.Expr == "" {
		return nil, nil, fmt.Errorf("缺少参数 expr")
	}
	value, err := tool.CalculateSimple(input.Expr)
	if err != nil {
		return nil, nil, fmt.Errorf("计算失败: %w", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("%s=%g", input.Expr, value),
			},
		},
	}, nil, nil
}

