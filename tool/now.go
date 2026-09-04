package tool

import (
	"context"
	"encoding/json"
	"time"
)

type Now struct {
}

func (n *Now) Name() string {
	return "now"
}
func (n *Now) Description() string {
	return "返回当前时间"
}
func (c *Now) Parameters() json.RawMessage {
	return []byte(`{
		"type": "object",
	}`)
}

func (c *Now) Call(ctx context.Context, args json.RawMessage) (string, error) {
	t := time.Now().String()
	return t, nil
}
