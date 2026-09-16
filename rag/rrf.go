package rag

import "sort"

// RRF 用倒数排名融合多路检索结果。每路是一个按相关度降序排列的文档 ID 列表。
// k 是平滑常数，经验值 60。返回融合后按总分降序的 ID。
func RRF(rankings [][]string, k int) []string {
	score := make(map[string]float64)
	for _, ranking := range rankings {
		for rank, id := range ranking {
			score[id] += 1.0 / float64(k+rank+1) // 排名越靠前(rank 越小)，加分越多
		}
	}
	ids := make([]string, 0, len(score))
	for id := range score {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return score[ids[i]] > score[ids[j]] })
	return ids
}
