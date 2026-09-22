package mas

import (
	"context"
	"fmt"
)

// SwarmResult 表示 Swarm 体系中单个 Agent 运行结束后的状态转移结果。
//
// 转移分支语义：
// 1. 若 HandoffTo 为空，说明当前 Agent 已经解决了问题，Answer 为交付给用户的最终答案。
// 2. 若 HandoffTo 非空，说明当前 Agent 判定问题超出了自己的能力范围（或需要下一阶段专家介入），
//    控制权将立即转交给目标名称为 HandoffTo 的 Agent，同时可将 Answer 作为中间处理记录传递下去。
type SwarmResult struct {
	Answer    string // 最终答案，或者转交给下一棒时的阶段性中间成果说明
	HandoffTo string // 转交目标：下一个接棒的 Agent 名称标识（为空表示执行终止）
}

// SwarmAgent 定义了 Swarm 群体协作网络中的一个自治节点。
type SwarmAgent struct {
	Name string                                                       // Agent 节点名称（如 "triage", "refund_specialist", "billing"）
	Run  func(ctx context.Context, input string) (SwarmResult, error) // 实际业务逻辑处理函数
}

// Swarm 实现了轻量级、去中心化的“接力转交（Handoff）”多智能体编排网络（源自 OpenAI Swarm 架构思想）。
//
// 核心设计：
// 1. 无中央调度器：没有任何一个 Master 或 Supervisor 在微观把控每一步。各 Agent 之间完全基于对等协议接力。
// 2. 状态上下文无缝沿袭：上一个 Agent 的处理总结会自动作为上下文追加到 input 中，带给下一个 Agent。
// 3. 最大跃点防御（MaxHops）：设置上限，杜绝多个 Agent 互相甩锅形成无限转交死循环。
type Swarm struct {
	Agents  map[string]SwarmAgent // 节点网络名册：名称 -> SwarmAgent
	MaxHops int                   // 最大允许的转交跃点数（超过该次数仍未落盘则强行熔断）
}

// Run 启动从起始 Agent (start) 开始的链式转交流程：
func (s *Swarm) Run(ctx context.Context, start, input string) (string, error) {
	cur := start
	for hop := 0; hop < s.MaxHops; hop++ {
		// 1. 查找当前承接任务的目标 Agent
		a, ok := s.Agents[cur]
		if !ok {
			return "", fmt.Errorf("不存在的 Agent: %q", cur)
		}

		// 2. 执行当前 Agent 逻辑
		res, err := a.Run(ctx, input)
		if err != nil {
			return "", err
		}

		// 3. 判断是否到达终止条件（没有产生转交意图，说明当前 Agent 给出了终态答案）
		if res.HandoffTo == "" {
			return res.Answer, nil
		}

		// 4. 接力传递：更新游标指向下一个 Agent，并将本环节的处理记录追加至上下文
		cur = res.HandoffTo
		if res.Answer != "" {
			input = input + "\n\n[" + a.Name + " 的处理]：" + res.Answer
		}
	}

	// 转交次数超限熔断
	return "", fmt.Errorf("转交超过 %d 次仍无最终答案", s.MaxHops)
}
