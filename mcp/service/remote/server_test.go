package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRemoteMCPServerOAuthAndScope(t *testing.T) {
	server := NewRemoteMCPServer("test-secret-key", "static-token")
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", server.HandleOAuthToken)
	mux.HandleFunc("/mcp", server.AuthMiddleware(server.HandleRPC))

	// 1. 无 Token 请求应该返回 401
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未携带 token 时期望 401, 实际: %d", rec.Code)
	}

	// 2. 申请只有 read 权限的 Token
	tokenRec := httptest.NewRecorder()
	tokenReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader("client_id=mcp-agent-client&client_secret=mcp-agent-secret&scope=mcp:tools:read"))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(tokenRec, tokenReq)
	if tokenRec.Code != http.StatusOK {
		t.Fatalf("申请 Token 失败: %d, body: %s", tokenRec.Code, tokenRec.Body.String())
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.NewDecoder(tokenRec.Body).Decode(&tokenResp)
	if tokenResp.AccessToken == "" {
		t.Fatalf("未能获取 access_token")
	}

	// 3. 使用只有 read 权限的 Token 读取工具列表（应该成功）
	listRec := httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)))
	listReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("读取工具列表失败: %d, body: %s", listRec.Code, listRec.Body.String())
	}

	// 4. 使用只有 read 权限的 Token 调用退款高危工具（应该被 403 拦截）
	callRec := httptest.NewRecorder()
	callReq := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"refund_order","arguments":{"order_id":"123","amount":100}}}`)))
	callReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	mux.ServeHTTP(callRec, callReq)
	if callRec.Code != http.StatusForbidden {
		t.Fatalf("缺少退款权限时期望 403, 实际: %d", callRec.Code)
	}

	// 5. 申请具备退款权限的 Token 并调用（应该成功）
	adminTokenRec := httptest.NewRecorder()
	adminTokenReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader("client_id=mcp-agent-client&client_secret=mcp-agent-secret&scope=mcp:tools:read%20mcp:tools:call:refund"))
	adminTokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(adminTokenRec, adminTokenReq)

	var adminTokenResp struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.NewDecoder(adminTokenRec.Body).Decode(&adminTokenResp)

	callAdminRec := httptest.NewRecorder()
	callAdminReq := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"refund_order","arguments":{"order_id":"123","amount":100}}}`)))
	callAdminReq.Header.Set("Authorization", "Bearer "+adminTokenResp.AccessToken)
	mux.ServeHTTP(callAdminRec, callAdminReq)
	if callAdminRec.Code != http.StatusOK {
		t.Fatalf("具备退款权限时期望 200, 实际: %d, body: %s", callAdminRec.Code, callAdminRec.Body.String())
	}
}

func TestVerifyJWTExpired(t *testing.T) {
	server := NewRemoteMCPServer("test-secret-key", "")
	expiredClaims := MCPClaims{
		Subject:   "test",
		ExpiresAt: time.Now().Add(-10 * time.Minute).Unix(),
	}
	token, err := server.signJWT(expiredClaims)
	if err != nil {
		t.Fatalf("signJWT error: %v", err)
	}

	_, err = server.verifyJWT(token)
	if err == nil || !strings.Contains(err.Error(), "过期") {
		t.Fatalf("过期 token 应校验失败，实际: %v", err)
	}
}
