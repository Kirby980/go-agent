package rag

import "context"

// Reranker：用交叉编码器对候选重新打分。Voyage rerank-2.5 是常用实现。
type Reranker interface {
	Rerank(ctx context.Context, query string, docs []Document, topN int) ([]Document, error)
}

// Retriever 把"混合检索 + rerank"封装成一次 Retrieve 调用。
type Retriever struct {
	Store         VectorStore
	Embedder      Embedder
	Reranker      Reranker
	// KeywordSearch 可选提供自定义全文检索实现。若为空且 Store 实现了 KeywordSearcher，则自动调用 Store 的全文检索。
	KeywordSearch func(ctx context.Context, query string, k int) ([]Document, error)
}

func (r *Retriever) Retrieve(ctx context.Context, query string, topN int) ([]Document, error) {
	// 1) 向量检索 top-50
	qemb, err := r.Embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	vecHits, err := r.Store.Search(ctx, qemb[0], 50)
	if err != nil {
		return nil, err
	}

	// 2) 关键词检索 top-50
	var kwHits []Document
	if r.KeywordSearch != nil {
		kwHits, err = r.KeywordSearch(ctx, query, 50)
		if err != nil {
			return nil, err
		}
	} else if ks, ok := r.Store.(KeywordSearcher); ok {
		kwHits, err = ks.SearchKeyword(ctx, query, 50)
		if err != nil {
			return nil, err
		}
	}

	// 3) 候选结果融合
	var candidates []Document
	const candidateN = 50
	if len(kwHits) == 0 {
		candidates = vecHits
	} else {
		// RRF 融合两路排名
		byID := make(map[string]Document)
		var vecRank, kwRank []string
		for _, d := range vecHits {
			vecRank = append(vecRank, d.ID)
			byID[d.ID] = d
		}
		for _, d := range kwHits {
			kwRank = append(kwRank, d.ID)
			byID[d.ID] = d
		}
		fusedIDs := RRF([][]string{vecRank, kwRank}, 60)
		for _, id := range fusedIDs {
			if len(candidates) >= candidateN {
				break
			}
			candidates = append(candidates, byID[id])
		}
	}
	if r.Reranker == nil {
		if len(candidates) > topN {
			candidates = candidates[:topN]
		}
		return candidates, nil // 没配 rerank 就直接返回融合结果
	}
	return r.Reranker.Rerank(ctx, query, candidates, topN)
}
