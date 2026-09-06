package main

import (
	"fmt"
	"os"

	"github.com/Kirby980/agent/llm"
	"github.com/Kirby980/agent/llm/claude"
	"github.com/Kirby980/agent/llm/detector"
	"github.com/Kirby980/agent/llm/openai"
)

type Config struct {
	key      string `yaml:"key"`
	Name     string `yaml:"name"`
	BaseURL  string `yaml:"base_url"`
	Supplier string `yaml:"supplier"`
}

func loadConfigFromEnv() Config {
	cfg := Config{
		Supplier: os.Getenv("supplier"),
		key:      os.Getenv("key"),
		Name:     os.Getenv("name"),
		BaseURL:  os.Getenv("base_url"),
	}
	// ponytail: env 没设就用兜底值，够本地跑；要多环境再上 yaml
	if cfg.key == "" {
		cfg.key = "sk-maQ7NRBTui9jYycBab2gC7osGnsCdyHGtUcwVeh9TOQJ7eKl"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://wzw.pp.ua/v1"
	}
	if cfg.Name == "" {
		cfg.Name = "自定义"
	}
	if cfg.Supplier == "" {
		cfg.Supplier = "openai"
	}
	return cfg
}

func BuildAll(cfg Config) map[string]llm.Provider {
	p := map[string]llm.Provider{}
	switch cfg.Supplier {
	case "openai":
		switch cfg.Name {
		case "openai":
			p["openai"] = openai.NewOpenAI(cfg.key)
		case "doubao":
			p["doubao"] = openai.NewDoubao(cfg.key)
		case "deepseek":
			p["deepseek"] = openai.NewDeepSeek(cfg.key)
		default:
			p[cfg.Name] = openai.NewOpenAICustom(cfg.Name, cfg.key, cfg.BaseURL)
		}
	case "claude":
		switch cfg.Name {
		case "claude":
			p["claude"] = claude.NewClaude(cfg.key)
		default:
			p[cfg.Name] = claude.NewClaudeCustom(cfg.Name, cfg.key, cfg.BaseURL)
		}
	}
	return p
}

// BuildProviders 结合跨平台本地探测与显式配置，构建出所有可用的 Provider 切片。
func BuildProviders(cfg Config) []llm.Provider {
	var providers []llm.Provider
	seen := make(map[string]bool)

	// 1. 跨平台自动发现本地凭据（macOS/Windows/Linux 环境变量、客户端配置、本地服务）
	detected := detector.DetectLocalCredentials()
	for _, cred := range detected {
		keyID := fmt.Sprintf("%s:%s", cred.BaseURL, cred.APIKey)
		if seen[keyID] {
			continue
		}
		seen[keyID] = true

		switch cred.Supplier {
		case "openai":
			providers = append(providers, openai.NewOpenAICustom(cred.Name, cred.APIKey, cred.BaseURL))
		case "claude":
			providers = append(providers, claude.NewClaudeCustom(cred.Name, cred.APIKey, cred.BaseURL))
		}
	}

	// 2. 注入手动配置或兜底 Provider
	manualProviders := BuildAll(cfg)
	for name, p := range manualProviders {
		keyID := fmt.Sprintf("%s:%s", cfg.BaseURL, cfg.key)
		if !seen[keyID] {
			seen[keyID] = true
			providers = append(providers, p)
		} else if len(providers) == 0 {
			providers = append(providers, p)
		}
		_ = name
	}

	return providers
}

