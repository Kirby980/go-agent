package rag

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PgVectorStore struct {
	pool *pgxpool.Pool
}

// NewPgVectorStore 创建基于 PostgreSQL 的向量存储
func NewPgVectorStore(ctx context.Context, connStr string) (*PgVectorStore, error) {
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres failed: %w", err)
	}
	return &PgVectorStore{pool: pool}, nil
}

// vecLiteral 把向量转成 pgvector 认识的字面量字符串 "[a,b,c]"。
func vecLiteral(v []float32) string {
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = fmt.Sprintf("%g", x)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func (s *PgVectorStore) Add(ctx context.Context, docID string, chunks []string, embs [][]float32) error {
	batch := make([][]any, len(chunks))
	for i := range chunks {
		batch[i] = []any{docID, chunks[i], vecLiteral(embs[i])}
	}
	for _, row := range batch {
		_, err := s.pool.Exec(ctx,
			`INSERT INTO kb_chunks (doc_id, content, embedding) VALUES ($1, $2, $3)`,
			row...)
		if err != nil {
			return err
		}
	}
	return nil
}

// Search 用余弦距离检索最相似的 k 个片段。<=> 是 pgvector 的余弦距离算子。
func (s *PgVectorStore) Search(ctx context.Context, queryEmb []float32, k int) ([]Document, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, doc_id, content, 1 - (embedding <=> $1) AS score
		 FROM kb_chunks
		 ORDER BY embedding <=> $1
		 LIMIT $2`,
		vecLiteral(queryEmb), k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []Document
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.ID, &d.DocID, &d.Content, &d.Score); err != nil {
			return nil, err
		}
		docs = append(docs, d)
	}
	return docs, rows.Err()
}
