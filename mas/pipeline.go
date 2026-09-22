package mas

import (
	"context"
	"sync"
)

// Stage 实现了基于 Go 泛型与 Channel 的经典流式处理流水线单阶段（Pipeline Stage）。
//
// 架构特性：
// 1. 类型安全（泛型）：输入流元素类型为 I，经转换函数 fn 加工后产出类型为 O 的输出流。
// 2. 协程池并发（Worker Pool）：为当前阶段启动固定数量（workers）的并发 Goroutine，提升 CPU 与 I/O 密集型任务吞吐。
// 3. 背压与流式传输：前置 Stage 产生的数据可实时流向后置 Stage，无需全量内存缓冲。
// 4. 优雅收尾：当上游 in 管道被发送端关闭且所有 Worker 处理完存量数据后，自动通过 sync.WaitGroup 关闭输出管道 out。
func Stage[I, O any](ctx context.Context, in <-chan I, workers int, fn func(context.Context, I) (O, error)) <-chan O {
	out := make(chan O)
	var wg sync.WaitGroup

	// 启动指定数量的工作协程进行并发流式处理
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range in {
				o, err := fn(ctx, item)
				if err != nil {
					// 过滤掉错误数据，或者在实际生产中可引入单独的 ErrChan 进行死信转储
					continue
				}
				select {
				case out <- o:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// 监听所有后台 Worker 的退出状态，收尾关闭下游通道
	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}
