package claude

import "test/agent/internal/llm"

func NewClaudeCustom(name string, apiKey string, baseURL string) llm.Provider {
	return New(Config{Name: name, BaseURL: baseURL, APIKey: apiKey})
}

// NewClaude 官方 Anthropic Provider。baseURL 不带 /v1，Chat 里会拼上 /v1/messages。
func NewClaude(apiKey string) llm.Provider {
	return New(Config{Name: "claude", BaseURL: "https://api.anthropic.com", APIKey: apiKey})
}
