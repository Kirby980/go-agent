package rag

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Kirby980/go-es/builder"
	"github.com/Kirby980/go-es/client"
	"github.com/Kirby980/go-es/config"
	esconst "github.com/Kirby980/go-es/const"
)

// ESVectorStore 基于 Kirby980/go-es 库实现的 Elasticsearch 向量与全文检索存储
type ESVectorStore struct {
	client    *client.Client
	indexName string
}

// NewESVectorStore 创建 ES 存储实例
func NewESVectorStore(addresses []string, indexName, username, password string) (*ESVectorStore, error) {
	opts := []config.Option{
		config.WithAddresses(addresses...),
		config.WithTimeout(10 * time.Second),
		config.WithInsecureSkipVerify(true),
		config.WithRetry(3, 200*time.Millisecond),
	}
	if username != "" {
		opts = append(opts, config.WithAuth(username, password))
	}

	esClient, err := client.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("init es client failed: %w", err)
	}

	return &ESVectorStore{
		client:    esClient,
		indexName: indexName,
	}, nil
}

// InitIndex 初始化索引（若不存在则创建带有 dense_vector 和 text 映射的索引）
func (s *ESVectorStore) InitIndex(ctx context.Context, dims int) error {
	exists, err := builder.NewIndexBuilder(s.client, s.indexName).Exists(ctx)
	if err != nil {
		return fmt.Errorf("check index existence failed: %w", err)
	}
	if exists {
		return nil
	}

	return builder.NewIndexBuilder(s.client, s.indexName).
		Shards(1).
		Replicas(0).
		AddProperty("doc_id", esconst.FieldTypeKeyword).
		AddProperty("content", esconst.FieldTypeText).
		AddProperty("content_vector", esconst.FieldTypeDenseVector, func(m map[string]any) {
			m["dims"] = dims
			m["similarity"] = "cosine"
			m["index"] = true
		}).
		Create(ctx)
}

// Add 批量写入切片内容与对应的向量（使用 BulkBuilder 高性能写入）
func (s *ESVectorStore) Add(ctx context.Context, docID string, chunks []string, embs [][]float32) error {
	if len(chunks) == 0 {
		return nil
	}
	if len(chunks) != len(embs) {
		return fmt.Errorf("chunks length %d != embeddings length %d", len(chunks), len(embs))
	}

	bulk := builder.NewBulkBuilder(s.client).Index(s.indexName)
	for i := range chunks {
		doc := map[string]any{
			"doc_id":         docID,
			"content":        chunks[i],
			"content_vector": embs[i],
		}
		// 自动生成 ID，并写入文档
		bulk.Add("", "", doc)
	}

	resp, err := bulk.Do(ctx)
	if err != nil {
		return fmt.Errorf("es bulk execute failed: %w", err)
	}
	if resp.HasErrors() {
		var errs []string
		for _, item := range resp.FailedItems() {
			errs = append(errs, fmt.Sprintf("id=%s error=%s", item.ID, item.Error.Reason))
		}
		return fmt.Errorf("es bulk contains errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Search 执行 KNN 向量语义检索 (ES 8.x/9.x)
func (s *ESVectorStore) Search(ctx context.Context, queryEmb []float32, k int) ([]Document, error) {
	numCandidates := k * 5
	if numCandidates < 50 {
		numCandidates = 50
	}

	resp, err := builder.NewSearchBuilder(s.client, s.indexName).
		KNN("content_vector", queryEmb, k, numCandidates).
		Source("doc_id", "content").
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("es knn search failed: %w", err)
	}

	docs := make([]Document, 0, len(resp.Hits.Hits))
	for _, hit := range resp.Hits.Hits {
		docID, _ := hit.Source["doc_id"].(string)
		content, _ := hit.Source["content"].(string)
		docs = append(docs, Document{
			ID:      hit.ID,
			DocID:   docID,
			Content: content,
			Score:   float32(hit.Score),
		})
	}
	return docs, nil
}

// SearchKeyword 执行 BM25 关键词全文检索
func (s *ESVectorStore) SearchKeyword(ctx context.Context, query string, k int) ([]Document, error) {
	resp, err := builder.NewSearchBuilder(s.client, s.indexName).
		Match("content", query).
		Size(k).
		Source("doc_id", "content").
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("es keyword search failed: %w", err)
	}

	docs := make([]Document, 0, len(resp.Hits.Hits))
	for _, hit := range resp.Hits.Hits {
		docID, _ := hit.Source["doc_id"].(string)
		content, _ := hit.Source["content"].(string)
		docs = append(docs, Document{
			ID:      hit.ID,
			DocID:   docID,
			Content: content,
			Score:   float32(hit.Score),
		})
	}
	return docs, nil
}

// Close 关闭 ES 客户端连接
func (s *ESVectorStore) Close() error {
	return s.client.Close()
}
