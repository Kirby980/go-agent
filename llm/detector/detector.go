// Package detector 提供跨平台（macOS、Windows、Linux）自动识别本地环境凭据的能力，
// 支持从系统环境变量、本地客户端配置（如 Claude Desktop）及本地服务（如 Ollama）中发现 API Key 和 BaseURL。
package detector

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// ProviderCredentials 保存自动识别出的模型供应商配置与凭据。
type ProviderCredentials struct {
	Supplier string // "openai" 或 "claude"
	Name     string // 识别出的标识名称，如 "env-openai", "claude-desktop", "local-ollama"
	APIKey   string
	BaseURL  string
}

// DetectLocalCredentials 自动扫描当前系统环境，收集可用的 LLM Provider 凭据列表。
func DetectLocalCredentials() []ProviderCredentials {
	var results []ProviderCredentials

	// 1. 优先探测环境变量
	if creds, ok := detectFromEnv(); ok {
		results = append(results, creds...)
	}

	// 2. 探测本地客户端配置（如 Claude Desktop）
	if cred, ok := detectClaudeDesktop(); ok {
		results = append(results, cred)
	}

	// 3. 探测本地部署模型服务（如 Ollama）
	if cred, ok := detectLocalServer(); ok {
		results = append(results, cred)
	}

	return results
}

// detectFromEnv 从系统环境变量读取 OpenAI / Codex 及 Claude / Anthropic 配置。
func detectFromEnv() ([]ProviderCredentials, bool) {
	var list []ProviderCredentials

	// OpenAI / Codex
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		baseURL := os.Getenv("OPENAI_BASE_URL")
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		list = append(list, ProviderCredentials{
			Supplier: "openai",
			Name:     "env-openai",
			APIKey:   key,
			BaseURL:  baseURL,
		})
	}

	// Claude / Anthropic
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		baseURL := os.Getenv("ANTHROPIC_BASE_URL")
		if baseURL == "" {
			baseURL = "https://api.anthropic.com/v1"
		}
		list = append(list, ProviderCredentials{
			Supplier: "claude",
			Name:     "env-claude",
			APIKey:   key,
			BaseURL:  baseURL,
		})
	}

	return list, len(list) > 0
}

// detectClaudeDesktop 针对当前操作系统（Darwin / Windows / Linux）定位 Claude Desktop 配置文件。
func detectClaudeDesktop() (ProviderCredentials, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return ProviderCredentials{}, false
	}

	var configPath string
	switch runtime.GOOS {
	case "darwin": // macOS
		configPath = filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	case "windows": // Windows
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		configPath = filepath.Join(appData, "Claude", "claude_desktop_config.json")
	case "linux": // Linux
		configDir, err := os.UserConfigDir()
		if err != nil {
			configDir = filepath.Join(home, ".config")
		}
		configPath = filepath.Join(configDir, "Claude", "claude_desktop_config.json")
	default:
		return ProviderCredentials{}, false
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return ProviderCredentials{}, false
	}

	// 解析其中的环境变量配置
	var raw struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(data, &raw); err == nil && raw.Env != nil {
		if key, exists := raw.Env["ANTHROPIC_API_KEY"]; exists && key != "" {
			baseURL := raw.Env["ANTHROPIC_BASE_URL"]
			if baseURL == "" {
				baseURL = "https://api.anthropic.com/v1"
			}
			return ProviderCredentials{
				Supplier: "claude",
				Name:     "claude-desktop",
				APIKey:   key,
				BaseURL:  baseURL,
			}, true
		}
	}

	return ProviderCredentials{}, false
}

// detectLocalServer 探测本机是否运行着免鉴权的本地模型（如 Ollama）。
func detectLocalServer() (ProviderCredentials, bool) {
	client := http.Client{Timeout: 300 * time.Millisecond}
	resp, err := client.Get("http://localhost:11434/api/version")
	if err == nil && resp.StatusCode == http.StatusOK {
		_ = resp.Body.Close()
		return ProviderCredentials{
			Supplier: "openai", // Ollama 兼容 OpenAI 格式接口
			Name:     "local-ollama",
			APIKey:   "ollama", // 占位 Key
			BaseURL:  "http://localhost:11434/v1",
		}, true
	}
	return ProviderCredentials{}, false
}
