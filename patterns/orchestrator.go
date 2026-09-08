package patterns

import (
	"context"
	"fmt"
	"strings"

	"github.com/Kirby980/agent/agent"
	"github.com/Kirby980/agent/llm"
)

type subtask struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type decomposition struct {
	Subtasks []subtask `json:"subtasks"`
}

type Orchestrator struct {
	Provider  llm.Provider
	Model     string
	NewWorker func() *agent.Agent // 工厂：每个子任务一个全新 worker，上下文互相隔离
}

func (o *Orchestrator) Run(ctx context.Context, goal string) (string, error) {
	// 1) 分解：让编排者把任务拆成子任务（结构化输出）
	system := "你是任务编排者。把用户任务拆解为若干可独立完成的子任务，" +
		"严格只输出 JSON：{\"subtasks\":[{\"id\":\"t1\",\"description\":\"...\"}]}"
	raw, err := complete(ctx, o.Provider, o.Model, system, goal)
	if err != nil {
		return "", err
	}
	d, err := llm.ParseInto[decomposition](raw)
	if err != nil {
		return "", fmt.Errorf("分解结果无法解析: %w", err)
	}
	if len(d.Subtasks) == 0 {
		return "", fmt.Errorf("编排者未产出任何子任务")
	}

	// 2) 并行执行：每个子任务一个独立 worker（上下文隔离，互不污染）
	tasks := make([]func(context.Context) (string, error), len(d.Subtasks))
	for i, st := range d.Subtasks {
		st := st // 捕获循环变量（Go 1.22+ 已自动，这里显式以兼容旧版）
		tasks[i] = func(ctx context.Context) (string, error) {
			worker := o.NewWorker()
			res, err := worker.Run(ctx, st.Description)
			if err != nil {
				return "", fmt.Errorf("子任务 %s 失败: %w", st.ID, err)
			}
			return fmt.Sprintf("[%s] %s\n%s", st.ID, st.Description, res), nil
		}
	}
	results, err := Sectioning(ctx, tasks)
	if err != nil {
		return "", err
	}

	// 3) 综合：把各 worker 的结果交给编排者整合成最终回答
	synthSystem := "下面是各子任务的执行结果，请综合成对原始任务的完整、连贯的回答。"
	return complete(ctx, o.Provider, o.Model, synthSystem,
		"原始任务：\n"+goal+"\n\n子任务结果：\n"+strings.Join(results, "\n\n"))
}
