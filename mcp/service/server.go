package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

func main() {
	r := bufio.NewReader(os.Stdin)
	w := os.Stdout
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		var req struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(line, &req) != nil {
			continue
		}
		switch req.Method {
		case "initialize":
			reply(w, req.ID, map[string]any{
				"protocolVersion": "2025-11-25",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "demo", "version": "0.1.0"},
			})
		case "tools/list":
			reply(w, req.ID, map[string]any{"tools": []map[string]any{{
				"name":        "get_time",
				"description": "返回当前时间",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			}}})
		case "tools/call":
			reply(w, req.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": time.Now().Format(time.RFC3339)}},
			})
			// notifications/initialized 等通知无 id，无需回复
		}
	}
}

func reply(w io.Writer, id *int, result any) {
	if id == nil {
		return
	}
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	fmt.Fprintf(w, "%s\n", data)
}
