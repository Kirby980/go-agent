package ctxeng

import (
	"sort"
	"strings"

	"github.com/Kirby980/agent/tool"
)

// SelectTools 按与 query 的相关度，从全部工具里筛出最相关的 maxN 个，避免一次性全塞给模型。
// 这里用描述与名称分词匹配示意；生产建议用工具描述的 embedding 做语义筛选。
func SelectTools(query string, all []tool.Tool, maxN int) []tool.Tool {
	if maxN <= 0 || len(all) == 0 {
		return []tool.Tool{}
	}
	q := strings.ToLower(query)
	type scored struct {
		t tool.Tool
		s int
	}
	ranked := make([]scored, 0, len(all))
	for _, t := range all {
		s := 0
		name := strings.ToLower(t.Name())
		if strings.Contains(q, name) {
			s += 5
		}
		desc := strings.ToLower(t.Description())
		// 检查分词匹配（适合英文或带空格词组）
		for _, w := range strings.Fields(desc) {
			if w != "" && strings.Contains(q, w) {
				s += 2
			}
		}
		// 检查 2-gram 匹配（友好支持中文关键词匹配）
		runes := []rune(desc)
		for i := 0; i < len(runes)-1; i++ {
			bigram := string(runes[i : i+2])
			if strings.Contains(q, bigram) {
				s += 1
			}
		}
		ranked = append(ranked, scored{t, s})
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].s > ranked[j].s })
	out := make([]tool.Tool, 0, maxN)
	for i := 0; i < len(ranked) && i < maxN; i++ {
		out = append(out, ranked[i].t)
	}
	return out
}

// SearchTools 是 SelectTools 的兼容别名
func SearchTools(query string, all []tool.Tool, maxN int) []tool.Tool {
	return SelectTools(query, all, maxN)
}
