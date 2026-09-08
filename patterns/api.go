package patterns

import (
	"context"

	"github.com/Kirby980/agent/llm"
)

func complete(ctx context.Context, p llm.Provider, model, system, user string) (string, error) {
	resp, err := p.Chat(ctx, llm.ChatRequest{
		Model: model,
		Messages: []llm.Message{
			{
				Role: llm.RoleSystem, Content: system,
			},
			{
				Role: llm.RoleUser, Content: user,
			},
		},
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}
