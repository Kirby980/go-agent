package plan

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/Kirby980/agent/tool"
)

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
