package plan

import (
	"context"
	"fmt"
	"sync"

	"github.com/Kirby980/agent/tool"
)

// Execute 接收一个声明式的 DAG 任务图 Plan 与工具注册表 reg，按照拓扑分层推进执行。
//
// 并发与治理模型：
// 1. 拓扑分层驱动（Levels）：
//    - 首先计算出执行层次 levels，例如：level 0 -> level 1 -> level 2。
//    - 只有当 level 0 中的所有并行任务执行成功后，才会同步推进到 level 1。
// 2. 层内极致并发（Intra-Level Concurrency）：
//    - 同一 level 内的所有任务在逻辑上互不依赖，因此使用 Goroutine + sync.WaitGroup 全量并发调度。
// 3. 快速故障熔断（Fail-Fast via Context Cancellation）：
//    - 为每个 level 分配一个可撤销的派生 context (lctx, cancel)。
//    - 一旦层内任意一个任务返回错误（如工具不存在或工具执行失败）：
//      利用 sync.Once 捕获首个错误并立刻调用 cancel()，向同层仍在运行的其他 Goroutine 发送中断信号，避免无效计算。
// 4. 并发安全结果收集：
//    - 使用 sync.Mutex 保护结果哈希表 results，记录每个 Task ID 到其输出文本的映射。
//
// 返回值：
// 包含所有已完成 Task ID 对应输出的 map，若中途失败则返回已完成部分结果及触发熔断的错误。
func Execute(ctx context.Context, p Plan, reg *tool.Registry) (map[string]string, error) {
	// 1. 拓扑分层与环路校验
	levels, err := Levels(p)
	if err != nil {
		return nil, err
	}

	// 2. 建立任务 ID 快速查找索引
	byID := make(map[string]Task, len(p.Tasks))
	for _, t := range p.Tasks {
		byID[t.ID] = t
	}

	results := make(map[string]string)
	var mu sync.Mutex // 互斥锁：保护 results 映射的并发写入

	// 3. 逐层依次推进执行
	for _, level := range levels {
		var wg sync.WaitGroup
		var firstErr error
		var once sync.Once
		lctx, cancel := context.WithCancel(ctx) // 本层专属 context：层内任一任务失败立即取消同层其余任务

		// 4. 层内任务并发分发
		for _, id := range level {
			t := byID[id]
			wg.Add(1)
			go func(t Task) {
				defer wg.Done()

				// 检查并获取对应工具
				tl, ok := reg.Get(t.Tool)
				if !ok {
					once.Do(func() {
						firstErr = fmt.Errorf("工具 %q 不存在", t.Tool)
						cancel() // 触发本层快速熔断
					})
					return
				}

				// 带着本层 context 调用真实工具执行
				out, err := tl.Call(lctx, t.Args)
				if err != nil {
					once.Do(func() {
						firstErr = err
						cancel() // 触发本层快速熔断
					})
					return
				}

				// 线程安全记录执行产物
				mu.Lock()
				results[t.ID] = out
				mu.Unlock()
			}(t)
		}

		// 等待本层所有任务 Goroutine 收尾
		wg.Wait()
		cancel() // 释放本层 context 资源

		// 若本层发生错误，阻断后续所有层级的执行，立即返回
		if firstErr != nil {
			return results, firstErr
		}
	}
	return results, nil
}
