package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kirby980/agent/schema"
)

type TypedTool[T any] struct {
	name string
	desc string
	fn   func(ctx context.Context, args T) (string, error)
}

func NewTypedTool[T any](name, desc string, fn func(ctx context.Context, args T) (string, error)) *TypedTool[T] {
	return &TypedTool[T]{
		name: name,
		desc: desc,
		fn:   fn,
	}
}

func (t *TypedTool[T]) Name() string {
	return t.name
}

func (t *TypedTool[T]) Description() string {
	return t.desc
}

func (t *TypedTool[T]) Parameters() json.RawMessage {
	var zero T
	s := schema.Generate(zero)
	return schema.MustJson(s)
}

// Call 自动把模型给的 JSON 参数解析成 T，再调用业务函数。
func (t *TypedTool[T]) Call(ctx context.Context, raw json.RawMessage) (string, error) {
	var args T
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("工具 %q 参数解析失败: %w", t.name, err)
		}
	}
	return t.fn(ctx, args)
}
