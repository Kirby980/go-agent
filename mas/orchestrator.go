package mas

import (
	"context"
	"fmt"
	"strings"

	"github.com/Kirby980/agent/agent"
	"github.com/Kirby980/agent/llm"
	"github.com/Kirby980/agent/patterns"
)

// IsolatedOrchestrator 实现了“分治编排与上下文物理隔离（Divide-and-Conquer with Context Isolation）”模式。
//
// 核心痛点与解决思路：
// 在单 Agent 模式下，当任务复杂庞大时，所有子任务的搜索、工具调试、执行失败尝试都会塞在同一个上下文历史中，
// 极易导致：
// 1. 上下文长度迅速超标（Token 成本暴涨）；
// 2. 注意力稀释（Lost in the middle）：模型被前序无关细节带偏；
//
// 本编排器的方案：
// 1. 主编排者拆分任务：仅输出抽象的高维子任务描述与指派角色。
// 2. 上下文强隔离：每个子任务通过 NewSubagent 获得一个干净、独立的全新 Agent 运行，其内部工具调用细节不外溢。
// 3. 浓缩结论汇总：子 Agent 执行完成后只向主编排者交回精炼的最终结论，主编排者基于浓缩结论完成最终综合回答。
type IsolatedOrchestrator struct {
	Provider    llm.Provider                    // 编排者主模型出口
	Model       string                          // 编排者模型名称
	NewSubagent func(role string) *agent.Agent // 角色工厂：每次调用都现造一个拥有专属上下文的全新 Agent 实例
}

// subtask 记录单个拆解后的子任务及其指派角色
type subtask struct {
	Role string `json:"role"` // 执行该子任务的角色名称（如 "database_expert", "security_auditor"）
	Task string `json:"task"` // 该角色需要完成的具体目标说明
}

// plan 记录编排者分解出的全套子任务清单
type plan struct {
	Subtasks []subtask `json:"subtasks"`
}

// Run 启动任务拆解、隔离执行与高维归纳流程：
func (o *IsolatedOrchestrator) Run(ctx context.Context, goal string) (string, error) {
	// 阶段 1：编排者分解任务（结构化 JSON 输出）
	sys := "你是编排者。把任务拆成可独立完成的子任务，为每个子任务指定一个角色。" +
		"严格只输出 JSON：{\"subtasks\":[{\"role\":\"...\",\"task\":\"...\"}]}"
	raw, err := chat(ctx, o.Provider, o.Model, sys, goal)
	if err != nil {
		return "", err
	}
	p, err := llm.ParseInto[plan](raw)
	if err != nil {
		return "", fmt.Errorf("分解结果无法解析: %w", err)
	}

	// 阶段 2：并行运行各子任务。每个子任务拥有完全独立的全新 Agent，上下文物理隔离
	jobs := make([]func(context.Context) (string, error), len(p.Subtasks))
	for i, st := range p.Subtasks {
		st := st
		jobs[i] = func(ctx context.Context) (string, error) {
			sub := o.NewSubagent(st.Role) // 实例化一个全新、隔离的 Subagent
			result, err := sub.Run(ctx, st.Task)
			if err != nil {
				return "", fmt.Errorf("子代理[%s]失败: %w", st.Role, err)
			}
			// 只带回浓缩后的结论，过滤掉子 Agent 内部可能产生的大量工具调用中间过程
			return fmt.Sprintf("【%s 的结论】%s", st.Role, result), nil
		}
	}

	// 利用 Sectioning 并发调度所有子任务，等待全量就绪
	results, err := patterns.Sectioning(ctx, jobs)
	if err != nil {
		return "", err
	}

	// 阶段 3：编排者仅基于各角色的“浓缩结论”统一归纳输出
	// 编排者的主上下文始终轻量、高信息密度，绝不会被子任务的代码检索或试错日志所污染
	return chat(ctx, o.Provider, o.Model,
		"下面是各子代理的结论，请综合成对原始目标的完整回答。",
		"目标："+goal+"\n\n各子代理结论：\n"+strings.Join(results, "\n\n"))
}
