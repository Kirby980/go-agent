package main

import (
	"os"
	"test/agent/internal/llm"
	"test/agent/internal/llm/claude"
	"test/agent/internal/llm/openai"
)

type Config struct {
	key      string `yaml:"key"`
	Name     string `yaml:name`
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
