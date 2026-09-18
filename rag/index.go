package rag

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
)

// IndexDir 遍历本地目录中的 .md 与 .txt 文件，切分并写入存储
func IndexDir(ctx context.Context, dir string, ch *RecursiveChunker, emb Embedder, store VectorStore) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if ext := filepath.Ext(path); ext != ".md" && ext != ".txt" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		chunks := ch.Split(string(data))
		if len(chunks) == 0 {
			return nil
		}
		embs, err := emb.Embed(ctx, chunks) // 批量向量化
		if err != nil {
			return err
		}
		return store.Add(ctx, path, chunks, embs)
	})
}
