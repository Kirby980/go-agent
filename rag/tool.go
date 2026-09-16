package rag

import (
	"context"
	"fmt"
	"strings"

	"github.com/Kirby980/agent/tool"
)

type kbSearchArgs struct {
	Query string `json:"query" desc:"要在知识库中检索的查询。请提炼出精准的检索关键词或问题。"`
}

// SearchTool 把检索管线包装成一个 Agent 工具。
func SearchTool(r *Retriever) tool.Tool {
	return tool.NewTypedTool(
		"search_knowledge_base",
		"在企业知识库中检索资料。当你需要依据公司政策、产品文档或历史资料回答时调用；"+
			"可以用不同的查询多次调用以获取更全面的信息。",
		func(ctx context.Context, a kbSearchArgs) (string, error) {
			if r == nil {
				return "暂未配置知识库", nil
			}
			docs, err := r.Retrieve(ctx, a.Query, 5)
			if err != nil {
				return "", err
			}
			if len(docs) == 0 {
				return "知识库中未找到相关资料。", nil
			}
			var sb strings.Builder
			for i, d := range docs {
				// 带上来源标注，便于模型在回答里引用、也便于事后审计
				fmt.Fprintf(&sb, "[来源 %d | doc=%s | 相关度=%.2f]\n%s\n\n",
					i+1, d.DocID, d.Score, d.Content)
			}
			return sb.String(), nil
		})
}
