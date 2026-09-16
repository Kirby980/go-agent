package rag

import (
	"context"
	"fmt"
)

// Document 代表从知识库召回的切片文档
type Document struct {
	ID      string  `json:"id"`
	DocID   string  `json:"doc_id"`
	Content string  `json:"content"`
	Score   float32 `json:"score"` // 检索时回填的相关度分数
}

// VectorStore 通用向量数据库接口
type VectorStore interface {
	// Add 将文档分块和对应的稠密向量批量写入存储
	Add(ctx context.Context, docID string, chunks []string, embs [][]float32) error
	// Search 通过查询向量检索最相似的 Top-K 文档
	Search(ctx context.Context, queryEmb []float32, k int) ([]Document, error)
}

// KeywordSearcher 关键字全文检索接口（若底层引擎支持，如 ES）
type KeywordSearcher interface {
	SearchKeyword(ctx context.Context, query string, k int) ([]Document, error)
}

// StoreType 向量数据库类型
type StoreType string

const (
	StoreTypeES StoreType = "es" // 默认：Elasticsearch
	StoreTypePG StoreType = "pg" // PostgreSQL (pgvector)
)

// StoreConfig 向量存储初始化配置
type StoreConfig struct {
	Type StoreType `json:"type"`

	// Elasticsearch 配置
	ESHost     string `json:"es_host"`     // 默认 "http://localhost:9200"
	ESIndex    string `json:"es_index"`    // 默认 "kb_chunks"
	ESUsername string `json:"es_username"`
	ESPassword string `json:"es_password"`

	// PostgreSQL 配置
	PGConn string `json:"pg_conn"` // 如 "postgres://user:pass@localhost:5432/mydb?sslmode=disable"
}

// NewVectorStore 工厂函数：根据配置返回具体向量库实例（默认 ES）
func NewVectorStore(ctx context.Context, cfg StoreConfig) (VectorStore, error) {
	switch cfg.Type {
	case StoreTypePG:
		if cfg.PGConn == "" {
			return nil, fmt.Errorf("pg_conn is required for PostgreSQL vector store")
		}
		return NewPgVectorStore(ctx, cfg.PGConn)
	case StoreTypeES, "":
		host := cfg.ESHost
		if host == "" {
			host = "http://localhost:9200"
		}
		index := cfg.ESIndex
		if index == "" {
			index = "kb_chunks"
		}
		return NewESVectorStore([]string{host}, index, cfg.ESUsername, cfg.ESPassword)
	default:
		return nil, fmt.Errorf("unsupported vector store type: %s", cfg.Type)
	}
}
