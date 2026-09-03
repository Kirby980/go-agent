// Package tool 定义了 Agent 工具契约规范（Tool interface）以及工具注册表（Registry）。
package tool

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/Kirby980/agent/llm"
)

// Tool 定义了 Agent 可调用的工具契约。
type Tool interface {
	// Name 返回工具的唯一名称（例如 "calculator", "web_search"）
	Name() string
	// Description 返回工具功能说明，模型根据此信息判断何时调用
	Description() string
	// Parameters 返回参数 JSON Schema，保留原始 JSON
	Parameters() json.RawMessage
	// Call 执行具体工具逻辑，返回给模型的观察文本 Observation
	Call(ctx context.Context, args json.RawMessage) (string, error)
}

// Registry 是工具的注册表，支持线程安全按名称查找与批量导出模型定义。
type Registry struct {
	tools map[string]Tool
}

// NewRegistry 创建工具注册表，自动过滤空项。
func NewRegistry(tools ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool, len(tools))}
	for _, item := range tools {
		if item == nil {
			continue
		}
		r.tools[item.Name()] = item
	}
	return r
}

// Get 按工具名检索已注册的工具。
func (r *Registry) Get(name string) (Tool, bool) {
	if r == nil {
		return nil, false
	}
	item, ok := r.tools[name]
	return item, ok
}

// All 返回所有已注册工具，按工具名升序排列以保证确定性输出。
func (r *Registry) All() []Tool {
	if r == nil {
		return nil
	}
	out := make([]Tool, 0, len(r.tools))
	for _, item := range r.tools {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// ToolDefs 将注册表中的所有工具转换为 LLM 接口规范的 ToolDef 列表，供 Function Calling 请求使用。
func (r *Registry) ToolDefs() []llm.ToolDef {
	tools := r.All()
	defs := make([]llm.ToolDef, 0, len(tools))
	for _, item := range tools {
		defs = append(defs, llm.ToolDef{
			Name:        item.Name(),
			Description: item.Description(),
			Parameters:  item.Parameters(),
		})
	}
	return defs
}

