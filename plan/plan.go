// Package plan 提供了基于有向无环图（DAG）的任务编排、拓扑分层调度与并发执行器。
package plan

import "encoding/json"

// Task 表示 DAG 规划中的一个独立任务节点。
type Task struct {
	ID        string          `json:"id"`         // 任务唯一标识
	Tool      string          `json:"tool"`       // 要调用的工具名称
	Args      json.RawMessage `json:"args"`       // 传入工具的原始参数 JSON
	DependsOn []string        `json:"depends_on"` // 依赖的前置任务 ID 列表
}

// Plan 表示由一组有依赖关系的 Task 构成的执行图（DAG）。
type Plan struct {
	Tasks []Task `json:"tasks"`
}
