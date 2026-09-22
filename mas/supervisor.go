package mas

import (
	"context"
	"fmt"
	"strings"

	"github.com/Kirby980/agent/agent"
	"github.com/Kirby980/agent/llm"
)

// SupervisorDecision 记录团队主管大模型的单步决策输出（结构化 JSON 解析目标）。
type SupervisorDecision struct {
	Next   string `json:"next"`   // 下一步指派的 Worker 成员名称；若填 "FINISH" 则代表整项大任务已圆满结束
	Reason string `json:"reason"` // 主管做出该分派决定的推导依据与理由
}

// Supervisor 实现了经典的中心化“主管-工作者（Supervisor-Worker）”多 Agent 协作架构。
//
// 架构角色：
// 1. 主管（Supervisor）：作为中央调度大脑，持有全局目标与所有成员的技能名册。它不亲自做具体脏活累活，
//    而是审视当前已经取得的进展（progress），动态决定“下一步该派谁干活”或“是否大功告成（FINISH）”。
// 2. 工人（Workers）：各司其职的专能 Agent（例如 coder、tester、reviewer），接收指派的子任务与已有进展执行。
type Supervisor struct {
	Provider llm.Provider                                                       // 用于主管思考决策的模型出口
	Model    string                                                             // 主管使用的模型名称
	Workers  map[string]*agent.Agent                                            // 团队成员花名册：成员名 -> 对应的独立 Agent 实例
	MaxTurns int                                                                // 允许主管调度的最大轮次数（防止无限推诿或决策死循环）
	OnStep   func(turn int, decision SupervisorDecision, result string)        // 每轮主管决策与工人执行的回调通知（可选）
}

// decide 驱动主管模型审视任务目标与历史累积进展，做出下一步分派决策。
func (s *Supervisor) decide(ctx context.Context, task, progress string) (SupervisorDecision, error) {
	names := make([]string, 0, len(s.Workers))
	for n := range s.Workers {
		names = append(names, n)
	}
	system := fmt.Sprintf(
		"你是团队主管。可调度的成员有：%s。根据任务和当前进展，决定下一步该谁来做；"+
			"若任务已完成则输出 FINISH。严格只输出 JSON：{\"next\":\"成员名或FINISH\",\"reason\":\"理由\"}",
		strings.Join(names, ", "))
	out, err := chat(ctx, s.Provider, s.Model, system, "任务："+task+"\n\n当前进展：\n"+progress)
	if err != nil {
		return SupervisorDecision{}, err
	}
	return llm.ParseInto[SupervisorDecision](out)
}

// Run 启动 Supervisor-Worker 主管协同执行循环：
//
// 流程图解：
//
//     ┌─────────────────────────────────────────────────────────┐
//     │                      初始 Task 输入                      │
//     └────────────────────────────┬────────────────────────────┘
//                                  ▼
//     ┌─────────────────────────────────────────────────────────┐
//  ┌─►│ 1. 主管 decide(task, progress)：基于已有进展决定下一步谁来做│
//  │  └────────────────────────────┬────────────────────────────┘
//  │                               ▼
//  │                        d.Next == "FINISH"?
//  │                         ├── 是 ──► [结束退出，返回 progress 累积成果]
//  │                         └── 否
//  │                               ▼
//  │  ┌─────────────────────────────────────────────────────────┐
//  │  │ 2. 找到对应 Worker：w.Run(task + 已有进展 progress)       │
//  │  └────────────────────────────┬────────────────────────────┘
//  │                               ▼
//  │  ┌─────────────────────────────────────────────────────────┐
//  └──┤ 3. 产出 result 追加到 progress：【Worker名称】+ 结果文本   │
//     └─────────────────────────────────────────────────────────┘
func (s *Supervisor) Run(ctx context.Context, task string) (string, error) {
	// progress 用于在内存中跨轮次滚动记录各 Worker 的阶段性执行产物
	var progress strings.Builder

	for turn := 0; turn < s.MaxTurns; turn++ {
		// 1. 主管观察全局任务与最新累积的 progress 进展，判断下一步调度动作
		d, err := s.decide(ctx, task, progress.String())
		if err != nil {
			return "", err
		}

		if s.OnStep != nil {
			s.OnStep(turn, d, "")
		}

		// 2. 主管判定全部任务已达成，直接交付最终的全部累积产物
		if strings.EqualFold(d.Next, "FINISH") {
			return progress.String(), nil
		}

		// 3. 校验并获取主管选定的具体工人 Agent (忽略大小写)
		var worker *agent.Agent
		var workerName string
		for name, w := range s.Workers {
			if strings.EqualFold(name, d.Next) {
				worker = w
				workerName = name
				break
			}
		}
		if worker == nil {
			return "", fmt.Errorf("主管选了不存在的成员: %q", d.Next)
		}

		// 4. 将原始任务目标与至今为止的完整进展拼接后喂给选中的 Worker
		result, err := worker.Run(ctx, task+"\n\n已有进展：\n"+progress.String())
		if err != nil {
			return "", err
		}

		if s.OnStep != nil {
			s.OnStep(turn, d, result)
		}

		// 5. 核心进展累加：将本轮 Worker 的身份标签及其产出结果追加到 progress 末尾
		fmt.Fprintf(&progress, "【%s】\n%s\n\n", workerName, result)
	}

	// 达到最大轮次仍未由主管给出 FINISH 信号，返回当前已收集到的进展并报错提示
	return progress.String(), fmt.Errorf("达到最大轮次 %d 仍未 FINISH", s.MaxTurns)
}
