package patterns

import (
	"context"
	"fmt"

	"github.com/Kirby980/agent/llm"
)

type ChainStep struct {
	Name   string                   // 步骤名，用于报错定位
	System string                   // 本步的系统提示词
	Build  func(prev string) string // 用上一步输出构造本步用户输入
	Gate   func(out string) error   // 可选：校验本步输出，返回 err 则中止链
}

func RunChain(ctx context.Context, p llm.Provider, model, input string, steps []ChainStep) (string, error) {
	cur := input
	for _, s := range steps {
		out, err := complete(ctx, p, model, s.System, s.Build(cur))
		if err != nil {
			return "", fmt.Errorf("步骤 %q调用失败: %w", s.Name, err)
		}
		if s.Gate != nil {
			if err := s.Gate(out); err != nil {
				return "", fmt.Errorf("步骤 %q 校验未通过: %w", s.Name, err)
			}
		}
		cur = out
	}
	return cur, nil
}
