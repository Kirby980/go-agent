package rag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestESVectorStore_Mock(t *testing.T) {
	// 启动一个 mock ES 服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/_bulk":
			// 响应 Bulk 操作
			resp := map[string]any{
				"took":   10,
				"errors": false,
				"items": []any{
					map[string]any{
						"index": map[string]any{
							"_index": "test_kb",
							"_id":    "item1",
							"status": 201,
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case "/test_kb/_search":
			// 响应 KNN / Match 检索
			resp := map[string]any{
				"took":      5,
				"timed_out": false,
				"hits": map[string]any{
					"total": map[string]any{
						"value":    1,
						"relation": "eq",
					},
					"max_score": 0.95,
					"hits": []any{
						map[string]any{
							"_index": "test_kb",
							"_id":    "chunk_1",
							"_score": 0.95,
							"_source": map[string]any{
								"doc_id":  "doc_100",
								"content": "微信支付遇到 40301 错误码时需重新更新商户证书。",
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case "/test_kb":
			// HEAD 检查索引是否存在，返回 200
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"acknowledged": true})

		default:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		}
	}))
	defer server.Close()

	ctx := context.Background()

	// 1. 创建基于 go-es 的 ESVectorStore
	store, err := NewESVectorStore([]string{server.URL}, "test_kb", "", "")
	if err != nil {
		t.Fatalf("NewESVectorStore failed: %v", err)
	}
	defer store.Close()

	// 2. 测试 Add 批量写入
	chunks := []string{"微信支付遇到 40301 错误码时需重新更新商户证书。"}
	embs := [][]float32{{0.1, 0.2, 0.3}}
	err = store.Add(ctx, "doc_100", chunks, embs)
	if err != nil {
		t.Fatalf("store.Add failed: %v", err)
	}

	// 3. 测试 Search KNN 向量检索
	docs, err := store.Search(ctx, []float32{0.1, 0.2, 0.3}, 5)
	if err != nil {
		t.Fatalf("store.Search failed: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if docs[0].DocID != "doc_100" {
		t.Errorf("expected doc_100, got %s", docs[0].DocID)
	}

	// 4. 测试 SearchKeyword 全文检索
	kwDocs, err := store.SearchKeyword(ctx, "40301", 5)
	if err != nil {
		t.Fatalf("store.SearchKeyword failed: %v", err)
	}
	if len(kwDocs) != 1 {
		t.Fatalf("expected 1 kwDoc, got %d", len(kwDocs))
	}

	// 5. 测试工厂方法 NewVectorStore 默认 ES
	factoryStore, err := NewVectorStore(ctx, StoreConfig{
		ESHost:  server.URL,
		ESIndex: "test_kb",
	})
	if err != nil {
		t.Fatalf("NewVectorStore default failed: %v", err)
	}
	if factoryStore == nil {
		t.Fatal("factoryStore is nil")
	}
}
