package patterns

import (
	"context"
	"strings"

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

func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		if len(lines) >= 2 && strings.HasPrefix(lines[0], "```") {
			lines = lines[1:]
		}
		if len(lines) >= 1 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
			lines = lines[:len(lines)-1]
		}
		s = strings.TrimSpace(strings.Join(lines, "\n"))
	}
	return s
}
