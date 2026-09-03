package plan

import (
	"context"
	"fmt"
	"sync"

	"github.com/Kirby980/agent/tool"
)

// Execute 按拓扑层级执行计划，层内并行。返回每个任务 ID 到其输出的映射。
func Execute(ctx context.Context, p Plan, reg *tool.Registry) (map[string]string, error) {
	levels, err := Levels(p)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]Task, len(p.Tasks))
	for _, t := range p.Tasks {
		byID[t.ID] = t
	}

	results := make(map[string]string)
	var mu sync.Mutex // 保护 results 的并发写

	for _, level := range levels {
		var wg sync.WaitGroup
		var firstErr error
		var once sync.Once
		lctx, cancel := context.WithCancel(ctx) // 本层任一失败即取消其余

		for _, id := range level {
			t := byID[id]
			wg.Add(1)
			go func(t Task) {
				defer wg.Done()

				tl, ok := reg.Get(t.Tool)
				if !ok {
					once.Do(func() { firstErr = fmt.Errorf("工具 %q 不存在", t.Tool); cancel() })
					return
				}
				out, err := tl.Call(lctx, t.Args)
				if err != nil {
					once.Do(func() { firstErr = err; cancel() })
					return
				}
				mu.Lock()
				results[t.ID] = out
				mu.Unlock()
			}(t)
		}

		wg.Wait()
		cancel() // 释放本层 context
		if firstErr != nil {
			return results, firstErr
		}
	}
	return results, nil
}
