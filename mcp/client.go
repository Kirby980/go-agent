package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

type StdioClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
	nextID int
}

// NewStdioClient 启动 MCP Server 子进程并建立管道。
func NewStdioClient(command string, args ...string) (*StdioClient, error) {
	cmd := exec.Command(command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr // 把 Server 的日志透传到我们的 stderr，方便调试
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &StdioClient{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		nextID: 1,
	}, nil
}

func (c *StdioClient) Close() error {
	_ = c.stdin.Close()
	return c.cmd.Wait()
}

func (c *StdioClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID
	c.nextID++

	data, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(c.stdin, "%s\n", data); err != nil {
		return nil, err
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line, err := c.stdout.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue // 防御性容错：跳过非 JSON-RPC 行
		}
		if resp.ID == nil || *resp.ID != id {
			continue // 通知或别的请求响应
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("MCP 错误 %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

// notify 发送不需要响应的通知。
func (c *StdioClient) notify(method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	data, _ := json.Marshal(msg)
	_, err := fmt.Fprintf(c.stdin, "%s\n", data)
	return err
}
