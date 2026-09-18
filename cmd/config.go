package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/Kirby980/agent/llm"
	"github.com/Kirby980/agent/llm/claude"
	"github.com/Kirby980/agent/llm/detector"
	"github.com/Kirby980/agent/llm/openai"
)

// RAGConfig 知识库配置
type RAGConfig struct {
	Enabled        bool   `yaml:"enabled"`
	StoreType      string `yaml:"store_type"` // "pg" 或 "es"，默认 "pg" (或回落 es)
	PGConn         string `yaml:"pg_conn"`    // PostgreSQL 数据库连接串
	ESHost         string `yaml:"es_host"`
	ESIndex        string `yaml:"es_index"`
	ESUsername     string `yaml:"es_username"`
	ESPassword     string `yaml:"es_password"`
	EmbeddingURL   string `yaml:"embedding_url"`
	EmbeddingKey   string `yaml:"embedding_key"`
	EmbeddingModel string `yaml:"embedding_model"`
	EmbeddingDim   int    `yaml:"embedding_dim"`
}

// Config 核心客户端配置
type Config struct {
	Model    string    `yaml:"model"`
	BaseURL  string    `yaml:"base_url"`
	APIKey   string    `yaml:"api_key"`
	Supplier string    `yaml:"supplier"`
	Name     string    `yaml:"name"`
	RAG      RAGConfig `yaml:"rag"`
}

// LoadConfig 自动加载配置文件，并支持环境变量动态覆盖
func LoadConfig() (Config, string, error) {
	var cfg Config
	var loadedPath string

	homeDir, _ := os.UserHomeDir()
	candidatePaths := []string{
		"./agent.yaml",         // 当前目录项目级配置（最高优先级）
		"./.agent/config.yaml", // 当前目录隐藏配置
		filepath.Join(homeDir, ".agent", "config.yaml"), // 用户主目录全局配置
	}

	for _, p := range candidatePaths {
		if data, err := os.ReadFile(p); err == nil {
			if err := yaml.Unmarshal(data, &cfg); err == nil {
				loadedPath = p
				break
			}
		}
	}

	if loadedPath == "" {
		cfg = loadConfigFromEnv()
		loadedPath = "内置默认值 / 环境变量"
	}

	// 环境变量优先级高于文件配置
	if envModel := os.Getenv("MODEL"); envModel != "" {
		cfg.Model = envModel
	}
	if envBaseURL := os.Getenv("BASE_URL"); envBaseURL != "" {
		cfg.BaseURL = envBaseURL
	}
	if envAPIKey := os.Getenv("API_KEY"); envAPIKey != "" {
		cfg.APIKey = envAPIKey
	}
	if envSupplier := os.Getenv("SUPPLIER"); envSupplier != "" {
		cfg.Supplier = envSupplier
	}

	return cfg, loadedPath, nil
}

func loadConfigFromEnv() Config {
	cfg := Config{
		Model:    os.Getenv("model"),
		Supplier: os.Getenv("supplier"),
		APIKey:   os.Getenv("key"),
		Name:     os.Getenv("name"),
		BaseURL:  os.Getenv("base_url"),
	}

	if cfg.Model == "" {
		cfg.Model = "gemini-2.5-pro"
	}
	if cfg.APIKey == "" {
		cfg.APIKey = ""
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:4000/v1"
	}
	if cfg.Name == "" {
		cfg.Name = "vertex-gateway"
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
			p["openai"] = openai.NewOpenAI(cfg.APIKey)
		case "doubao":
			p["doubao"] = openai.NewDoubao(cfg.APIKey)
		case "deepseek":
			p["deepseek"] = openai.NewDeepSeek(cfg.APIKey)
		default:
			p[cfg.Name] = openai.NewOpenAICustom(cfg.Name, cfg.APIKey, cfg.BaseURL)
		}
	case "claude":
		switch cfg.Name {
		case "claude":
			p["claude"] = claude.NewClaude(cfg.APIKey)
		default:
			p[cfg.Name] = claude.NewClaudeCustom(cfg.Name, cfg.APIKey, cfg.BaseURL)
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
		keyID := fmt.Sprintf("%s:%s", cfg.BaseURL, cfg.APIKey)
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
