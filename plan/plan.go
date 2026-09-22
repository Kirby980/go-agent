// Package plan 提供了基于有向无环图（DAG, Directed Acyclic Graph）的任务编排框架。
//
// 核心设计思想：
// 1. 静态图声明：通过 Task 与 DependsOn 显式定义任务之间的因果依赖链条。
// 2. 拓扑分层调度：利用 Levels 算法将任务划分为若干无相互依赖的“执行层（Level）”。
// 3. 层内并发执行：同层任务利用 Goroutine 并发加速，跨层任务严格等待前置依赖完成。
// 4. 快速熔断机制：层内任意任务失败即通过 context.WithCancel 终止同层剩余计算，避免浪费资源。
package plan

import "encoding/json"

// Task 表示 DAG 任务规划中的一个最小可执行单元（任务节点）。
// 每个 Task 绑定一个目标工具及调用参数，并显式指明其执行所必须依赖的前置任务。
type Task struct {
	ID        string          `json:"id"`         // 任务唯一标识（不可重复，用于构建依赖拓扑图）
	Tool      string          `json:"tool"`       // 该任务需要调用的工具名称（对应 tool.Registry 中的注册名称）
	Args      json.RawMessage `json:"args"`       // 传入工具的原始参数 JSON（延迟解析）
	DependsOn []string        `json:"depends_on"` // 依赖的前置任务 ID 列表（必须等待这些任务全部成功后才能启动当前任务）
}

// Plan 表示由一组相互关联的 Task 构成的全局执行计划图。
// 必须是有向无环图（DAG），不能包含循环依赖，否则在拓扑分层阶段会报错拒绝执行。
type Plan struct {
	Tasks []Task `json:"tasks"` // 任务节点集合
}
