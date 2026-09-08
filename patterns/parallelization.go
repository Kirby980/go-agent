package patterns

import (
	"context"
	"sync"

	"github.com/Kirby980/agent/llm"
)

// Sectioning 并行执行一批互不依赖的任务，按原顺序返回结果；任一失败即整体失败。
func Sectioning(ctx context.Context, tasks []func(context.Context) (string, error)) ([]string, error) {
	results := make([]string, len(tasks))
	var wg sync.WaitGroup
	var firstErr error
	var once sync.Once

	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for i, task := range tasks {
		wg.Add(1)
		go func(i int, task func(context.Context) (string, error)) {
			defer wg.Done()
			out, err := task(cctx)
			if err != nil {
				once.Do(func() {
					firstErr = err
					cancel()
				})
				return
			}
			results[i] = out
		}(i, task)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

// Voting 用相同提示词独立跑 n 次，返回 n 个结果，交给上层聚合。
func Voting(ctx context.Context, p llm.Provider, model, system, user string, n int) ([]string, error) {
	tasks := make([]func(context.Context) (string, error), n)
	for i := range tasks {
		tasks[i] = func(ctx context.Context) (string, error) {
			return complete(ctx, p, model, system, user)
		}
	}
	return Sectioning(ctx, tasks)
}

// Majority 返回出现次数最多的结果，适合"是/否"或有限类别的投票。
func Majority(votes []string) string {
	counts := make(map[string]int)
	best, bestN := "", 0
	for _, v := range votes {
		counts[v]++
		if counts[v] > bestN {
			best, bestN = v, counts[v]
		}
	}
	return best
}
