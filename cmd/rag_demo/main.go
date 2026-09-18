package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Kirby980/agent/rag"
)

func main() {
	mode := flag.String("mode", "compare", "运行模式: index (索引本地文档), compare (检索对比实验)")
	dir := flag.String("dir", "./docs", "本地知识库文档目录")
	query := flag.String("q", "请简要介绍公司的年假和报销政策？", "查询测试问题")
	pgConn := flag.String("pg", "postgres://agent:agent_password@localhost:5432/agent_kb?sslmode=disable", "PostgreSQL 连接串")
	embURL := flag.String("emb-url", "http://localhost:4000/v1", "Embedding 服务地址")
	embKey := flag.String("emb-key", "sk-yangzenghe-gemini", "Embedding API Key")
	embModel := flag.String("emb-model", "text-embedding-004", "Embedding 模型")
	embDim := flag.Int("emb-dim", 768, "Embedding 维度")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fmt.Println("=== 初始化 RAG 组件 ===")
	store, err := rag.NewPgVectorStore(ctx, *pgConn)
	if err != nil {
		fmt.Printf("连接 PostgreSQL 失败: %v\n", err)
		os.Exit(1)
	}

	embedder := rag.NewOpenAIEmbedder(*embURL, *embKey, *embModel, *embDim)
	chunker := rag.NewRecursiveChunker(300, 30)

	switch *mode {
	case "index":
		fmt.Printf("正在从目录 %s 索引文档 (.md / .txt)...\n", *dir)
		if err := rag.IndexDir(ctx, *dir, chunker, embedder, store); err != nil {
			fmt.Printf("索引失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("索引完成！")

	case "compare":
		fmt.Printf(">>> 待检索问题: %s\n\n", *query)

		// 1. 纯向量检索
		fmt.Println("-------------------------------------------")
		fmt.Println("【实验 1: 纯向量检索 (Dense Retrieval)】")
		qemb, err := embedder.Embed(ctx, []string{*query})
		if err != nil {
			fmt.Printf("生成 Query 向量失败: %v\n", err)
			return
		}
		vecDocs, err := store.Search(ctx, qemb[0], 5)
		if err != nil {
			fmt.Printf("向量检索失败: %v\n", err)
			return
		}
		for i, d := range vecDocs {
			fmt.Printf("[%d] 来源: %s (相似度: %.4f)\n内容摘要: %s\n\n", i+1, d.DocID, d.Score, truncate(d.Content, 80))
		}

		// 2. 混合检索 + 重排 (Hybrid + RRF + Rerank)
		fmt.Println("-------------------------------------------")
		fmt.Println("【实验 2: 混合检索 + RRF + Rerank (Hybrid Search)】")
		retriever := &rag.Retriever{
			Store:    store,
			Embedder: embedder,
		}
		hybridDocs, err := retriever.Retrieve(ctx, *query, 5)
		if err != nil {
			fmt.Printf("混合检索失败: %v\n", err)
			return
		}
		for i, d := range hybridDocs {
			fmt.Printf("[%d] 来源: %s (综合评分: %.4f)\n内容摘要: %s\n\n", i+1, d.DocID, d.Score, truncate(d.Content, 80))
		}

	default:
		fmt.Printf("未知模式: %s\n", *mode)
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
