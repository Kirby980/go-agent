package patterns

import "context"

// Evaluation 是评估者的结构化输出。
type Evaluation struct {
	Pass     bool   `json:"pass"`     // 是否达标
	Score    int    `json:"score"`    // 0-100 分
	Feedback string `json:"feedback"` // 不达标时的具体修改意见
}

// Generator 根据上一轮反馈生成内容（首轮 feedback 为空）。
type Generator func(ctx context.Context, feedback string) (string, error)

// Evaluator 评估一份内容，给出是否达标与反馈。
type Evaluator func(ctx context.Context, output string) (Evaluation, error)

// EvaluatorOptimizer 生成→评估→（不达标则带反馈重生成）循环，至多 maxRounds 轮。
// 返回最终内容、最后一次评估、错误。
func EvaluatorOptimizer(ctx context.Context, gen Generator, eval Evaluator, maxRounds int) (string, Evaluation, error) {
	var output string
	var ev Evaluation
	feedback := ""
	for round := 0; round < maxRounds; round++ {
		var err error
		output, err = gen(ctx, feedback)
		if err != nil {
			return "", ev, err
		}
		ev, err = eval(ctx, output)
		if err != nil {
			return "", ev, err
		}
		if ev.Pass {
			return output, ev, nil // 达标，提前返回
		}
		feedback = ev.Feedback // 带着意见进入下一轮
	}
	return output, ev, nil // 用尽轮次：返回最后一版（注意 ev.Pass 可能为 false）
}
