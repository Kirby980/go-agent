package ctxeng

import (
	"context"

	"github.com/Kirby980/agent/llm"
)

func Compact(ctx context.Context, messages []llm.Message, keepRecent int,
	summarize func(ctx context.Context, older []llm.Message) (string, error)) ([]llm.Message, error) {
	if keepRecent < 0 {
		keepRecent = 0
	}
	if len(messages) <= keepRecent+1 {
		return messages, nil
	}
	system := messages[0]                           // 系统提示词放到[0]
	older := messages[1 : len(messages)-keepRecent] // 较早部分需要压缩的
	recent := messages[len(messages)-keepRecent:]   // 最近的keepRecent保留
	summary, err := summarize(ctx, older)
	if err != nil {
		return nil, err
	}
	out := make([]llm.Message, 0, 2+keepRecent)
	out = append(out, system)
	out = append(out, llm.Message{Role: llm.RoleUser, Content: "【早前对话摘要，据此延续】\n" + summary})
	out = append(out, recent...)
	return out, nil
}
