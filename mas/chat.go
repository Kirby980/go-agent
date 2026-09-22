package mas

import (
	"context"

	"github.com/Kirby980/agent/llm"
)

// chat 是 MAS 体系内部各多 Agent 调度器复用的极简单轮 LLM 对话辅助函数。
// 接收系统提示词 system 与用户输入 user，发起非流式请求并直接返回模型生成的纯文本内容。
func chat(ctx context.Context, p llm.Provider, model, system, user string) (string, error) {
	resp, err := p.Chat(ctx, llm.ChatRequest{
		Model: model,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: system},
			{Role: llm.RoleUser, Content: user},
		},
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}
