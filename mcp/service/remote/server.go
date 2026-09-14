package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type contextKey string

const (
	claimsContextKey contextKey = "mcp_claims"
)

// MCPClaims 定义 OAuth 2.0 JWT 负载结构
type MCPClaims struct {
	Issuer    string   `json:"iss,omitempty"`
	Subject   string   `json:"sub,omitempty"`
	Audience  string   `json:"aud,omitempty"`
	ExpiresAt int64    `json:"exp,omitempty"`
	IssuedAt  int64    `json:"iat,omitempty"`
	Scopes    []string `json:"scope,omitempty"` // 权限范围
}

type RemoteMCPServer struct {
	jwtSecret   []byte // 用于 HMAC-SHA256 签发与验签的密钥
	staticToken string // 可选的兜底静态 Token
}

func NewRemoteMCPServer(secretKey string, staticToken string) *RemoteMCPServer {
	return &RemoteMCPServer{
		jwtSecret:   []byte(secretKey),
		staticToken: staticToken,
	}
}

// verifyJWT 校验标准 HS256 JWT Token（零外部依赖实现）
func (s *RemoteMCPServer) verifyJWT(tokenString string) (*MCPClaims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("token 格式错误")
	}

	headerB64, payloadB64, sigB64 := parts[0], parts[1], parts[2]
	signingInput := headerB64 + "." + payloadB64

	mac := hmac.New(sha256.New, s.jwtSecret)
	mac.Write([]byte(signingInput))
	expectedSig := mac.Sum(nil)

	actualSig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, fmt.Errorf("签名解码失败: %w", err)
	}

	if !hmac.Equal(actualSig, expectedSig) {
		return nil, fmt.Errorf("签名验证不匹配")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, fmt.Errorf("负载解码失败: %w", err)
	}

	var claims MCPClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("解析 claims 失败: %w", err)
	}

	if claims.ExpiresAt > 0 && time.Now().Unix() > claims.ExpiresAt {
		return nil, fmt.Errorf("token 已过期")
	}

	return &claims, nil
}

// signJWT 辅助函数：签发测试用 HS256 JWT
func (s *RemoteMCPServer) signJWT(claims MCPClaims) (string, error) {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerBytes, _ := json.Marshal(header)
	payloadBytes, _ := json.Marshal(claims)

	headerB64 := base64.RawURLEncoding.EncodeToString(headerBytes)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signingInput := headerB64 + "." + payloadB64

	mac := hmac.New(sha256.New, s.jwtSecret)
	mac.Write([]byte(signingInput))
	sigB64 := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return signingInput + "." + sigB64, nil
}

// AuthMiddleware 支持 OAuth 2.0 Bearer JWT 与静态 Token 两种认证
func (s *RemoteMCPServer) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, `{"error":"missing or invalid Authorization header"}`, http.StatusUnauthorized)
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))

		// 1. 若配置了静态 Token 且匹配，直接放行并赋予全局权限
		if s.staticToken != "" && token == s.staticToken {
			ctx := context.WithValue(r.Context(), claimsContextKey, &MCPClaims{
				Subject: "admin-static",
				Scopes:  []string{"*"},
			})
			next(w, r.WithContext(ctx))
			return
		}

		// 2. 校验 OAuth 2.0 JWT Token
		claims, err := s.verifyJWT(token)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"unauthorized: %s"}`, err.Error()), http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		next(w, r.WithContext(ctx))
	}
}

// hasScope 检查当前请求的 Claims 是否拥有特定权限 scope
func hasScope(r *http.Request, requiredScope string) bool {
	claims, ok := r.Context().Value(claimsContextKey).(*MCPClaims)
	if !ok || claims == nil {
		return false
	}
	for _, s := range claims.Scopes {
		if s == "*" || s == requiredScope || s == "mcp:admin" {
			return true
		}
	}
	return false
}

// HandleOAuthToken 模拟 OAuth 2.0 Token 颁发端点（Client Credentials 模式）
func (s *RemoteMCPServer) HandleOAuthToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	clientID := r.FormValue("client_id")
	clientSecret := r.FormValue("client_secret")
	// 也可以从 HTTP Basic Auth 读取 client credentials
	if u, p, ok := r.BasicAuth(); ok {
		clientID, clientSecret = u, p
	}

	// 演示鉴权逻辑：校验客户端 ID 和密码
	if clientID != "mcp-agent-client" || clientSecret != "mcp-agent-secret" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_client",
			"error_description": "client_id 或 client_secret 无效",
		})
		return
	}

	requestedScope := r.FormValue("scope")
	var scopes []string
	if requestedScope != "" {
		scopes = strings.Fields(requestedScope)
	} else {
		scopes = []string{"mcp:tools:read", "mcp:tools:call:refund"}
	}

	now := time.Now()
	claims := MCPClaims{
		Issuer:    "mcp-oauth-server",
		Subject:   clientID,
		ExpiresAt: now.Add(1 * time.Hour).Unix(),
		IssuedAt:  now.Unix(),
		Scopes:    scopes,
	}

	accessToken, err := s.signJWT(claims)
	if err != nil {
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   3600,
		"scope":        strings.Join(scopes, " "),
	})
}

// HandleRPC 处理 MCP JSON-RPC 核心协议
func (s *RemoteMCPServer) HandleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      *int            `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	var result any
	switch req.Method {
	case "initialize":
		result = map[string]any{
			"protocolVersion": "2025-11-25",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "remote-oauth-mcp-server", "version": "1.0.0"},
		}
	case "notifications/initialized":
		w.WriteHeader(http.StatusOK)
		return
	case "tools/list":
		// 校验读取工具列表权限
		if !hasScope(r, "mcp:tools:read") {
			http.Error(w, `{"error":"forbidden: 需要 mcp:tools:read 权限"}`, http.StatusForbidden)
			return
		}
		result = map[string]any{
			"tools": []map[string]any{
				{
					"name":        "refund_order",
					"description": "【高危操作】对指定订单执行退款",
					"inputSchema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"order_id": map[string]any{"type": "string", "description": "订单号"},
							"amount":   map[string]any{"type": "number", "description": "退款金额"},
						},
						"required": []string{"order_id", "amount"},
					},
				},
			},
		}
	case "tools/call":
		var callArgs struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		_ = json.Unmarshal(req.Params, &callArgs)

		// 细粒度权限校验：调用退款工具必须具备 mcp:tools:call:refund 权限
		if callArgs.Name == "refund_order" && !hasScope(r, "mcp:tools:call:refund") {
			http.Error(w, `{"error":"forbidden: 权限不足，需要 mcp:tools:call:refund 权限"}`, http.StatusForbidden)
			return
		}

		result = map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": fmt.Sprintf("订单退款成功，参数: %s", string(callArgs.Arguments))},
			},
		}
	}

	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}
	_ = json.NewEncoder(w).Encode(resp)
}

func main() {
	server := NewRemoteMCPServer("super-secret-jwt-signing-key", "fallback-static-token")

	// 1. OAuth 2.0 Token 颁发端点
	http.HandleFunc("/oauth/token", server.HandleOAuthToken)

	// 2. 携带 OAuth Bearer Token 访问的 MCP 服务端点
	http.HandleFunc("/mcp", server.AuthMiddleware(server.HandleRPC))

	fmt.Println("==================================================")
	fmt.Println("远程 OAuth MCP Server 启动在 :8080")
	fmt.Println("  - Token 端点: http://localhost:8080/oauth/token")
	fmt.Println("  - MCP RPC 端点: http://localhost:8080/mcp")
	fmt.Println("==================================================")
	_ = http.ListenAndServe(":8080", nil)
}
