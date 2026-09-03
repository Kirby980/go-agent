package plan

import "encoding/json"

type Task struct {
	ID        string          `json:"id"`         // 任务唯一标识
	Tool      string          `json:"tool"`       // 调用哪个工具
	Args      json.RawMessage `json:"args"`       // 工具参数
	DependsOn []string        `json:"depends_on"` // 依赖的前置任务 ID
}

type Plan struct {
	Tasks []Task `json:"tasks"`
}
