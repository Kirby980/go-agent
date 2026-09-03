package tool

import (
	"context"
	"encoding/json"
	"testing"
)

type sampleTool struct {
	name string
	desc string
}

func (s *sampleTool) Name() string                               { return s.name }
func (s *sampleTool) Description() string                        { return s.desc }
func (s *sampleTool) Parameters() json.RawMessage                { return json.RawMessage(`{}`) }
func (s *sampleTool) Call(ctx context.Context, args json.RawMessage) (string, error) {
	return "ok", nil
}

func TestRegistry(t *testing.T) {
	t1 := &sampleTool{name: "beta", desc: "second"}
	t2 := &sampleTool{name: "alpha", desc: "first"}

	reg := NewRegistry(t1, nil, t2)

	if _, ok := reg.Get("alpha"); !ok {
		t.Errorf("expected alpha to exist")
	}
	if _, ok := reg.Get("gamma"); ok {
		t.Errorf("expected gamma to not exist")
	}

	all := reg.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(all))
	}
	// All should be sorted alphabetically
	if all[0].Name() != "alpha" || all[1].Name() != "beta" {
		t.Errorf("expected sorted names [alpha, beta], got [%s, %s]", all[0].Name(), all[1].Name())
	}

	defs := reg.ToolDefs()
	if len(defs) != 2 {
		t.Fatalf("expected 2 defs, got %d", len(defs))
	}
	if defs[0].Name != "alpha" || defs[0].Description != "first" {
		t.Errorf("unexpected def: %+v", defs[0])
	}
}
