package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"golang.org/x/oauth2/clientcredentials"
)

// HTTPClient 实现了基于 HTTP 的远程 MCP 客户端，满足 Client 接口。
type HTTPClient struct {
	endpoint   string
	authToken  string
	httpClient *http.Client
	mu         sync.Mutex
	nextID     int
}

// NewHTTPClient 创建使用静态 Bearer Token 的远程 MCP 客户端。
func NewHTTPClient(endpoint, authToken string) *HTTPClient {
	return &HTTPClient{
		endpoint:   endpoint,
		authToken:  authToken,
		httpClient: &http.Client{},
		nextID:     1,
	}
}

// NewOAuthHTTPClient 创建基于 OAuth 2.0 Client Credentials 模式的远程 MCP 客户端。
// 底层由 golang.org/x/oauth2 自动发起 Token 申请并在请求头注入 Authorization: Bearer <access_token>，
// 并在 Token 即将过期时自动刷新。
func NewOAuthHTTPClient(endpoint, tokenURL, clientID, clientSecret string, scopes []string) *HTTPClient {
	cfg := clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     tokenURL,
		Scopes:       scopes,
	}
	return &HTTPClient{
		endpoint:   endpoint,
		httpClient: cfg.Client(context.Background()),
		nextID:     1,
	}
}

func (c *HTTPClient) Close() error {
	return nil
}

func (c *HTTPClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	c.mu.Unlock()

	bodyBytes, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, fmt.Errorf("序列化 MCP 请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("远程 MCP 服务返回状态码 %d", resp.StatusCode)
	}

	var rpcResp rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("解析 MCP 响应失败: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("MCP 错误 %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

func (c *HTTPClient) Initialize(ctx context.Context) (ServerInfo, error) {
	raw, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2025-11-25",
		"clientInfo":      map[string]any{"name": "agent-mcp-client", "version": "1.0"},
	})
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

func (c *HTTPClient) Initialized(ctx context.Context) error {
	_, err := c.call(ctx, "notifications/initialized", nil)
	return err
}

func (c *HTTPClient) ListTools(ctx context.Context) ([]MCPTool, error) {
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

func (c *HTTPClient) CallTool(ctx context.Context, name string, args json.RawMessage) (CallToolResult, error) {
	raw, err := c.call(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return CallToolResult{}, err
	}
	var res CallToolResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return CallToolResult{}, err
	}
	return res, nil
}
