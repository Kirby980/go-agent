package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Kirby980/agent/agent"
	"github.com/Kirby980/agent/patterns"
	"github.com/Kirby980/agent/router"
	"github.com/Kirby980/agent/tool"
)

func main() {
	// Ctrl+C → context 取消
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cfg := loadConfigFromEnv() // TODO: 读取 BASE_URL / API_KEY / MODEL

	// 强制退出守护：收到 SIGINT 后给出最多 3 秒用于清理，3 秒后强制退出。
	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
			return // runOnce 已完成，取消强制退出
		case <-timer.C:
			fmt.Fprintln(os.Stderr, "\n超时，强制退出")
			os.Exit(1)
		}
	}()

	if err := pattern(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "\n出错:", err)
		close(done)
		os.Exit(1)
	}
	close(done)
}

// runOnce: 构造请求 → transport.NewClient().Do(req) → 解析 choices[0].message.content 与 usage → 打印。
func runOnce(ctx context.Context, cfg Config) error {
	providers := BuildProviders(cfg)
	if len(providers) == 0 {
		return fmt.Errorf("未找到任何可用 Provider，请检查环境变量或配置")
	}

	fmt.Println("=== 当前已加载并接入轮询/容灾的 Provider 节点 ===")
	for i, p := range providers {
		fmt.Printf(" [%d] %s\n", i+1, p.Name())
	}
	fmt.Println("================================================")

	// 使用 RoundRobin 轮询调度，并在某节点失败时自动尝试下一个节点（容灾降级）
	r, err := router.New(router.NewRoundRobin(), providers...)
	if err != nil {
		return fmt.Errorf("初始化模型路由器失败: %w", err)
	}

	// 包装为单个 llm.Provider 注入 Agent
	clusterProvider := r.AsProvider("multi-provider-cluster")

	a := agent.New(clusterProvider, "grok-4.6", tool.NewRegistry(&tool.Calculator{}, &tool.Now{}), agent.WithStore(agent.NewFileStore("./store"), "test"))

	var name string
	inputCh := make(chan string)
	outputCh := make(chan string)
	defer func() {
		close(outputCh)
		close(inputCh)
	}()
	go func() {
		for {
			_, ok := <-outputCh
			if !ok {
				break
			}
			fmt.Println("输入你的问题:")
			fmt.Scan(&name)
			inputCh <- name
		}

	}()
	outputCh <- ""
	for {
		fmt.Print("> ")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case q, ok := <-inputCh:
			if !ok {
				return nil // stdin closed
			}
			if strings.TrimSpace(q) == "" {
				continue
			}
			fmt.Println("question:", q)
			for ev := range a.RunStream(ctx, q) {
				switch ev.Type {
				case agent.EventThought:
					fmt.Printf("\n[思考] %s\n", ev.Text)
				case agent.EventToolCall:
					fmt.Printf("[调用工具] %s(%s)\n", ev.Tool, ev.Args)
				case agent.EventToolResult:
					fmt.Printf("[工具结果] %s\n", ev.Text)
				case agent.EventAnswerDelta:
					fmt.Println(ev.Text)
				case agent.EventError:
					fmt.Fprintln(os.Stderr, "[错误]", ev.Text)
				case agent.EventDone:
					fmt.Println("[完成]")
					outputCh <- ""
				}
			}
		}
	}
}

func pattern(ctx context.Context, cfg Config) error {
	c := BuildProviders(cfg)
	resp, err := patterns.PGE(ctx, c[0], "grok-4.6", `func main() {
	// Ctrl+C → context 取消
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cfg := loadConfigFromEnv() // TODO: 读取 BASE_URL / API_KEY / MODEL

	// 强制退出守护：收到 SIGINT 后给出最多 3 秒用于清理，3 秒后强制退出。
	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
			return // runOnce 已完成，取消强制退出
		case <-timer.C:
			fmt.Fprintln(os.Stderr, "\n超时，强制退出")
			os.Exit(1)
		}
	}()

	if err := runOnce(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "\n出错:", err)
		close(done)
		os.Exit(1)
	}
	close(done)
}`, 3)
	if err != nil {
		return err
	}
	fmt.Println(resp)
	return nil
}
