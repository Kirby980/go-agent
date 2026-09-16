package rag

import (
	"context"
	"testing"
)

type mockEmbedder struct{}

func (m *mockEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return [][]float32{{0.1, 0.2}}, nil
}

func (m *mockEmbedder) Dim() int {
	return 2
}

type mockHybridStore struct {
	vecDocs []Document
	kwDocs  []Document
}

func (m *mockHybridStore) Add(ctx context.Context, docID string, chunks []string, embs [][]float32) error {
	return nil
}

func (m *mockHybridStore) Search(ctx context.Context, queryEmb []float32, k int) ([]Document, error) {
	return m.vecDocs, nil
}

func (m *mockHybridStore) SearchKeyword(ctx context.Context, query string, k int) ([]Document, error) {
	return m.kwDocs, nil
}

func TestRetriever_HybridWithRRF(t *testing.T) {
	store := &mockHybridStore{
		vecDocs: []Document{
			{ID: "doc-1", DocID: "manual", Content: "向量检索第一名", Score: 0.9},
			{ID: "doc-2", DocID: "manual", Content: "向量检索第二名", Score: 0.8},
		},
		kwDocs: []Document{
			{ID: "doc-3", DocID: "manual", Content: "关键词精准匹配第一名", Score: 10.0},
			{ID: "doc-1", DocID: "manual", Content: "向量检索第一名", Score: 5.0},
		},
	}

	retriever := &Retriever{
		Store:    store,
		Embedder: &mockEmbedder{},
	}

	ctx := context.Background()
	results, err := retriever.Retrieve(ctx, "测试问题", 3)
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}

	// doc-1 在两路都出现，RRF 分数应该最高
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].ID != "doc-1" {
		t.Errorf("expected doc-1 to rank first by RRF, got %s", results[0].ID)
	}
}
