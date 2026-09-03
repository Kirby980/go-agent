package tool

import (
	"context"
	"encoding/json"
	"sort"
	"test/agent/internal/llm"
)

type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage                                    // 参数 JSON Schema，保留原始 JSON
	Call(ctx context.Context, args json.RawMessage) (string, error) // 执行，返回给模型的观察文本
}

// Registry 是工具的注册表，按名字查找。
type Registry struct {
	tools map[string]Tool
}

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

func (r *Registry) Get(name string) (Tool, bool) {
	if r == nil {
		return nil, false
	}
	item, ok := r.tools[name]
	return item, ok
}

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
