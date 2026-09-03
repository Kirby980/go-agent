package openai

import "github.com/Kirby980/agent/llm"

// 复用第三节的 Provider，只是预置各家的 baseURL。

// NewOpenAI 创建官方 OpenAI 的 Provider 实例。
func NewOpenAI(apiKey string) llm.Provider {
	return New(Config{Name: "openai", BaseURL: "https://api.openai.com/v1", APIKey: apiKey})
}

// NewDeepSeek 创建 DeepSeek 官方 API 的 Provider 实例。
func NewDeepSeek(apiKey string) llm.Provider {
	return New(Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", APIKey: apiKey})
}

// NewDoubao 创建火山引擎豆包大模型的 Provider 实例。
func NewDoubao(apiKey string) llm.Provider {
	return New(Config{Name: "doubao", BaseURL: "https://ark.cn-beijing.volces.com/api/v3", APIKey: apiKey})
}

// NewQwen 创建阿里云通义千问兼容模式的 Provider 实例。
func NewQwen(apiKey string) llm.Provider {
	return New(Config{Name: "qwen", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", APIKey: apiKey})
}

// NewOpenAICustom 创建指向自定义 BaseURL 的 OpenAI 兼容 Provider（适用于自建网关或私有化部署模型）。
func NewOpenAICustom(name string, apiKey string, baseURL string) llm.Provider {
	return New(Config{Name: name, BaseURL: baseURL, APIKey: apiKey})
}
