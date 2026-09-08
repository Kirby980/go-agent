package patterns

import (
	"context"
	"fmt"
	"strings"

	"github.com/Kirby980/agent/llm"
)

type reviewPlan struct {
	Dimensions []string `json:"dimensions"`
}

type reviewResult struct {
	Dimension string `json:"dimension"`
	Review    string `json:"review"`
}

// PGE (Plan-Gen-Eval) 三层 Agent 代码审查流程
func PGE(ctx context.Context, p llm.Provider, model, code string, maxRounds int) (string, error) {
	if maxRounds == 0 {
		maxRounds = 3
	}

	for i := 0; i < maxRounds; i++ {
		// ==================== 1. Planner ====================
		plan, err := planReview(ctx, p, model, code)
		if err != nil {
			return "", fmt.Errorf("Planner 失败: %w", err)
		}

		// ==================== 2. Generator (并行审查) ====================
		tasks := make([]func(context.Context) (string, error), len(plan.Dimensions))
		for i, dim := range plan.Dimensions {
			tasks[i] = func(ctx context.Context) (string, error) {
				return reviewDimension(ctx, p, model, dim, code)
			}
		}

		// 这里 reviews 就是所有维度的审查结果，直接传给 EvaluatorOptimizer
		reviews, err := Sectioning(ctx, tasks)
		if err != nil {
			return "", fmt.Errorf("Generator 失败: %w", err)
		}
		report = "基于以上维度合并出的代码审查报告草稿:\n" + strings.Join(reviews, "\n")
		// ==================== 3. Evaluator ====================
		gen := func(ctx context.Context, feedback string) (string, error) {
			src := report
			if feedback != "" {
				src = feedback // 后续轮次携带评审反馈
			}
			return complete(ctx, p, model, "根据上一轮反馈生成内容（首轮 feedback 为空）", feedback)
		}

		evl := func(ctx context.Context, content string) (Evaluation, error) {
			system := `根据content 评估一份代码审查报告，给出是否达标与反馈,返回格式JSON：{"pass":"true/false","score":"0-100","feedback":"...}`
			resp, err := complete(ctx, p, model, system, content)
			if err != nil {
				return Evaluation{}, err
			}
			return llm.ParseInto[Evaluation](resp)
		}

		output, event, err := EvaluatorOptimizer(ctx, gen, evl, 3)
		if err != nil {
			return "", fmt.Errorf("Evaluator 失败: %w", err)
		}

		if event.Pass {
			return output, nil
		}
	}

	return "", nil
}

func planReview(ctx context.Context, p llm.Provider, model, code string) (reviewPlan, error) {
	system := `你是代码意图分类器。
请分析用户的代码，决定要审查的维度。
只输出 JSON：{"dimensions":["正确性","并发安全","错误处理","性能","可读性",...]}
维度要具体、可操作，不要泛泛而谈。`
	resp, err := complete(ctx, p, model, system, code)
	if err != nil {
		return reviewPlan{}, err
	}
	return llm.ParseInto[reviewPlan](resp)
}

func reviewDimension(ctx context.Context, p llm.Provider, model, dimension, code string) (string, error) {
	system := `你是代码审查专家。
严格按照维度审查代码，输出 JSON：{"dimension":"...","review":"..."}
review 部分要具体、可操作、带证据。`
	resp, err := complete(ctx, p, model, system, fmt.Sprintf("维度：%s\n代码：%s", dimension, code))
	if err != nil {
		return "", err
	}
	rev, err := llm.ParseInto[reviewResult](resp)
	if err != nil {
		return "", err
	}
	return rev.Review, nil
}
