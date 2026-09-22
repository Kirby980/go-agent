package plan

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/Kirby980/agent/tool"
)

// mockTool 用于单元测试的模拟工具
type mockTool struct {
	fn func(ctx context.Context, args json.RawMessage) (string, error)
}

func (m *mockTool) Name() string                               { return "mock" }
func (m *mockTool) Description() string                        { return "mock tool" }
func (m *mockTool) Parameters() json.RawMessage                { return json.RawMessage(`{}`) }
func (m *mockTool) Call(ctx context.Context, args json.RawMessage) (string, error) {
	if m.fn != nil {
		return m.fn(ctx, args)
	}
	return "ok", nil
}

// TestLevels_Normal 验证标准菱形依赖图的拓扑分层：
// A -> (B, C) -> D
// 预期分层：
// Level 0: [A]
// Level 1: [B, C] (两节点无互斥依赖，同层并行)
// Level 2: [D]
func TestLevels_Normal(t *testing.T) {
	p := Plan{
		Tasks: []Task{
			{ID: "A"},
			{ID: "B", DependsOn: []string{"A"}},
			{ID: "C", DependsOn: []string{"A"}},
			{ID: "D", DependsOn: []string{"B", "C"}},
		},
	}

	levels, err := Levels(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := [][]string{
		{"A"},
		{"B", "C"},
		{"D"},
	}

	if !reflect.DeepEqual(levels, expected) {
		t.Fatalf("got levels %v, want %v", levels, expected)
	}
}

// TestLevels_CycleDetection 验证循环依赖（死锁环路）检测：
// A 依赖 B，同时 B 依赖 A。必须报错拒绝执行，防止死循环。
func TestLevels_CycleDetection(t *testing.T) {
	p := Plan{
		Tasks: []Task{
			{ID: "A", DependsOn: []string{"B"}},
			{ID: "B", DependsOn: []string{"A"}},
		},
	}

	_, err := Levels(p)
	if err == nil {
		t.Fatalf("expected cycle error, got nil")
	}
}

// TestLevels_MissingDependency 验证引用了不存在的前置任务 ID 时，系统应立即拦截报错。
func TestLevels_MissingDependency(t *testing.T) {
	p := Plan{
		Tasks: []Task{
			{ID: "A", DependsOn: []string{"nonexistent"}},
		},
	}

	_, err := Levels(p)
	if err == nil {
		t.Fatalf("expected error for missing dependency, got nil")
	}
}

// TestExecute_Success 验证按拓扑层级推进执行真实任务流，且参数与产物映射正确。
func TestExecute_Success(t *testing.T) {
	reg := tool.NewRegistry(&mockTool{
		fn: func(ctx context.Context, args json.RawMessage) (string, error) {
			return "result:" + string(args), nil
		},
	})

	p := Plan{
		Tasks: []Task{
			{ID: "T1", Tool: "mock", Args: json.RawMessage(`"1"`)},
			{ID: "T2", Tool: "mock", Args: json.RawMessage(`"2"`), DependsOn: []string{"T1"}},
		},
	}

	res, err := Execute(context.Background(), p, reg)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if res["T1"] != `result:"1"` || res["T2"] != `result:"2"` {
		t.Fatalf("unexpected results: %v", res)
	}
}

// TestExecute_ToolError 验证当层内某个工具执行报错时，能够及时熔断并向外冒泡错误。
func TestExecute_ToolError(t *testing.T) {
	reg := tool.NewRegistry(&mockTool{
		fn: func(ctx context.Context, args json.RawMessage) (string, error) {
			return "", errors.New("tool exploded")
		},
	})

	p := Plan{
		Tasks: []Task{
			{ID: "T1", Tool: "mock"},
		},
	}

	_, err := Execute(context.Background(), p, reg)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}
