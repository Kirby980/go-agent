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

// PGE (Plan-Gen-Eval) 三层 Agent 流程（带 IntentRouter 前置分流）
// 输入若不是代码，分流至普通回答；若输入是代码，进入 Plan -> Gen -> Eval 审查流程。
func PGE(ctx context.Context, p llm.Provider, model, input string, maxRounds int) (string, error) {
	router := &IntentRouter{
		Provider: p,
		Model:    model,
		Routes: []Route{
			{
				Name:        "code_review",
				Description: "用户输入是代码或包含需要审查的代码片段，需要进行结构化代码审查、缺陷分析与改进建议",
				Handle: func(ctx context.Context, in string) (string, error) {
					return RunPGEReview(ctx, p, model, in, maxRounds)
				},
			},
			{
				Name:        "general_answer",
				Description: "用户输入不是代码（如日常问候、通用技术概念咨询、闲聊等），不需要代码审查，只需普通回答",
				Handle: func(ctx context.Context, in string) (string, error) {
					return GeneralAnswer(ctx, p, model, in)
				},
			},
		},
		Fallback: func(ctx context.Context, in string) (string, error) {
			// 分类异常或未匹配时，优雅降级走普通回答
			return GeneralAnswer(ctx, p, model, in)
		},
	}

	return router.Dispatch(ctx, input)
}

// GeneralAnswer 对非代码输入进行常规普通问答。
func GeneralAnswer(ctx context.Context, p llm.Provider, model, input string) (string, error) {
	system := "你是一个专业的人工智能技术助手。请针对用户的提问或内容，给出清晰、准确、有帮助的回答。"
	return complete(ctx, p, model, system, input)
}

// RunPGEReview 执行完整的 Plan-Gen-Eval 三层代码审查流程。
func RunPGEReview(ctx context.Context, p llm.Provider, model, code string, maxRounds int) (string, error) {
	if maxRounds == 0 {
		maxRounds = 3
	}

	var lastOutput string
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
		report := "基于以上维度合并出的代码审查报告草稿:\n" + strings.Join(reviews, "\n")
		// ==================== 3. Evaluator ====================
		currentReport := report
		gen := func(ctx context.Context, feedback string) (string, error) {
			system := "你是代码审查报告整合专家。请输出结构清晰、专业严谨的代码审查报告。"
			var userPrompt string
			if feedback == "" {
				userPrompt = fmt.Sprintf("请根据以下各维度的审查意见，整合并生成一份完整的代码审查报告：\n\n%s", currentReport)
			} else {
				userPrompt = fmt.Sprintf("上一版审查报告如下：\n\n%s\n\n评审反馈提出了以下修改意见：\n%s\n\n请根据反馈意见对审查报告进行针对性优化和完善，输出修改后的完整报告。", currentReport, feedback)
			}
			resp, err := complete(ctx, p, model, system, userPrompt)
			if err != nil {
				return "", err
			}
			currentReport = resp // 更新当前版本，后续轮次基于最新版本继续优化
			return resp, nil
		}

		evl := func(ctx context.Context, content string) (Evaluation, error) {
			system := `评估给出的代码审查报告，判断其是否达标并给出反馈。
严格只输出 JSON 格式（注意布尔值与数值类型）：
{"pass": true, "score": 85, "feedback": "修改建议..."}`
			resp, err := complete(ctx, p, model, system, content)
			if err != nil {
				return Evaluation{}, err
			}
			return llm.ParseInto[Evaluation](cleanJSON(resp))
		}

		output, event, err := EvaluatorOptimizer(ctx, gen, evl, 3)
		if err != nil {
			return "", fmt.Errorf("Evaluator 失败: %w", err)
		}
		lastOutput = output

		if event.Pass {
			return output, nil
		}
	}

	return lastOutput, nil
}

// PGEReview 与 RunPGEReview 等价，直接执行审查流程。
func PGEReview(ctx context.Context, p llm.Provider, model, code string, maxRounds int) (string, error) {
	return RunPGEReview(ctx, p, model, code, maxRounds)
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
	return llm.ParseInto[reviewPlan](cleanJSON(resp))
}

func reviewDimension(ctx context.Context, p llm.Provider, model, dimension, code string) (string, error) {
	system := `你是代码审查专家。
严格按照维度审查代码，输出 JSON：{"dimension":"...","review":"..."}
review 部分要具体、可操作、带证据。`
	resp, err := complete(ctx, p, model, system, fmt.Sprintf("维度：%s\n代码：%s", dimension, code))
	if err != nil {
		return "", err
	}
	rev, err := llm.ParseInto[reviewResult](cleanJSON(resp))
	if err != nil {
		return "", err
	}
	return rev.Review, nil
}
