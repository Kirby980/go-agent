package ctxeng

import (
	"context"

	"github.com/Kirby980/agent/llm"
)

type AssembleConfig struct {
	Budget     Budget
	KeepRecent int
	Summarize  func(ctx context.Context, older []llm.Message) (string, error)
}

func Assemble(ctx context.Context, messages []llm.Message, cfg AssembleConfig) ([]llm.Message, error) {
	history := joinContent(messages)
	for cfg.Budget.History > 0 && EstimateTokens(history) > cfg.Budget.History {
		compacted, err := Compact(ctx, messages, cfg.KeepRecent, cfg.Summarize)
		if err != nil {
			return nil, err
		}
		if len(compacted) >= len(messages) {
			break // 压不动了（已到 system + 最近 keepRecent），避免死循环
		}
		messages = compacted
		history = joinContent(messages)
	}
	return messages, nil
}
func JoinContent(msgs []llm.Message) string {
	var b []byte
	for _, m := range msgs {
		b = append(b, m.Content...)
		b = append(b, '\n')
	}
	return string(b)
}

func joinContent(msgs []llm.Message) string {
	return JoinContent(msgs)
}
