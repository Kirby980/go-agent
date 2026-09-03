package agent

import (
	"fmt"
	"strings"
)

const reactSystemTemplate = `你是一个会使用工具完成任务的助手。

你可以使用以下工具：
%s

每一轮必须严格使用以下格式之一：

Thought: 你的简短思考
Action: 要使用的工具名称
Action Input: 调用工具的 JSON 参数

或者在任务完成时输出：

Final Answer: 给用户的最终答案

工具执行结果会由程序作为 Observation 返回。`

type reactStep struct {
	Thought     string // 模型的简短思考
	Action      string // 要调用的工具
	ActionInput string // 工具参数 JSON 文本
	FinalAnswer string // 若非空，表示循环结束
}

func parseReact(text string) (reactStep, error) {
	var step reactStep
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	for _, line := range strings.Split(normalized, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Final Answer:"):
			step.FinalAnswer = strings.TrimSpace(strings.TrimPrefix(line, "Final Answer:"))
		case strings.HasPrefix(line, "Thought:"):
			step.Thought = strings.TrimSpace(strings.TrimPrefix(line, "Thought:"))
		case strings.HasPrefix(line, "Action:"):
			step.Action = strings.TrimSpace(strings.TrimPrefix(line, "Action:"))
		case strings.HasPrefix(line, "Action Input:"):
			step.ActionInput = strings.TrimSpace(strings.TrimPrefix(line, "Action Input:"))
		}
	}
	if step.FinalAnswer != "" {
		return step, nil
	}
	if step.Action == "" || step.ActionInput == "" {
		return step, fmt.Errorf("ReAct 解析失败：未找到 Action / Action Input")
	}
	return step, nil
}
