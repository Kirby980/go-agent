package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

type localRoundTripper struct {
	handler http.Handler
}

func (l *localRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	l.handler.ServeHTTP(rec, req)
	return rec.Result(), nil
}

func TestOAuthHTTPClient(t *testing.T) {
	mux := http.NewServeMux()

	// 1. Mock OAuth Token 端点
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		clientID := r.FormValue("client_id")
		clientSecret := r.FormValue("client_secret")
		if clientID != "test-client" || clientSecret != "test-secret" {
			http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "valid-oauth-jwt-token-12345",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	})

	// 2. Mock MCP 端点
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer valid-oauth-jwt-token-12345" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		var res any
		switch req.Method {
		case "initialize":
			res = map[string]any{"serverInfo": map[string]string{"name": "test-remote", "version": "1.0"}}
		case "tools/list":
			res = map[string]any{
				"tools": []map[string]any{
					{"name": "remote_echo", "description": "echo test"},
				},
			}
		case "tools/call":
			res = map[string]any{
				"content": []map[string]any{{"type": "text", "text": "hello from oauth remote mcp"}},
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": res})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mockTransport := &localRoundTripper{handler: mux}
	mockHTTP := &http.Client{Transport: mockTransport}

	// 把底层的网络请求走 mockHTTP（包含获取 Token 与访问 MCP 两个环节）
	oauthCtx := context.WithValue(ctx, oauth2.HTTPClient, mockHTTP)
	cfg := clientcredentials.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenURL:     "http://local-mcp/oauth/token",
		Scopes:       []string{"mcp:tools:read"},
	}

	client := &HTTPClient{
		endpoint:   "http://local-mcp/mcp",
		httpClient: cfg.Client(oauthCtx),
		nextID:     1,
	}

	// 测试 Initialize
	info, err := client.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}
	if info.Name != "test-remote" {
		t.Errorf("期望服务名 test-remote, 实际: %s", info.Name)
	}

	// 测试 ListTools
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools 失败: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "remote_echo" {
		t.Fatalf("工具列表不匹配: %+v", tools)
	}

	// 测试 CallTool
	result, err := client.CallTool(ctx, "remote_echo", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("CallTool 失败: %v", err)
	}
	if !strings.Contains(result.Text(), "hello from oauth remote mcp") {
		t.Errorf("返回内容不符合预期: %s", result.Text())
	}

	// 测试 BridgeAll 桥接
	bridged, err := BridgeAll(ctx, client)
	if err != nil {
		t.Fatalf("BridgeAll 失败: %v", err)
	}
	if len(bridged) != 1 {
		t.Fatalf("桥接工具数量不为 1: %d", len(bridged))
	}
	toolOut, err := bridged[0].Call(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("执行桥接工具失败: %v", err)
	}
	if !strings.Contains(toolOut, "hello from oauth remote mcp") {
		t.Errorf("桥接工具调用输出不匹配: %s", toolOut)
	}
}
