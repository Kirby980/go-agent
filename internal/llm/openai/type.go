package openai

import "test/agent/internal/llm"

// 复用第三节的 Provider，只是预置各家的 baseURL。

func NewOpenAI(apiKey string) llm.Provider {
	return New(Config{Name: "openai", BaseURL: "https://api.openai.com/v1", APIKey: apiKey})
}

// NewDeepSeek DeepSeek Provider
func NewDeepSeek(apiKey string) llm.Provider {
	return New(Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", APIKey: apiKey})
}

// NewDoubao 豆包 Provider
func NewDoubao(apiKey string) llm.Provider {
	return New(Config{Name: "doubao", BaseURL: "https://ark.cn-beijing.volces.com/api/v3", APIKey: apiKey})
}

// NewQwen 千问 Provider
func NewQwen(apiKey string) llm.Provider {
	return New(Config{Name: "qwen", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", APIKey: apiKey})
}

func NewOpenAICustom(name string, apiKey string, baseURL string) llm.Provider {
	return New(Config{Name: name, BaseURL: baseURL, APIKey: apiKey})
}
