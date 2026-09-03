package claude

import "github.com/Kirby980/agent/llm"

// NewClaudeCustom 创建指向自定义 BaseURL 的 Claude 兼容 Provider（适用于代理或自建端点）。
func NewClaudeCustom(name string, apiKey string, baseURL string) llm.Provider {
	return New(Config{Name: name, BaseURL: baseURL, APIKey: apiKey})
}

// NewClaude 创建官方 Anthropic Claude Provider（默认连接 https://api.anthropic.com）。
func NewClaude(apiKey string) llm.Provider {
	return New(Config{Name: "claude", BaseURL: "https://api.anthropic.com", APIKey: apiKey})
}
