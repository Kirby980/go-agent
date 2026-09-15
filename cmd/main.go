package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Kirby980/agent/agent"
	"github.com/Kirby980/agent/builtin"
	"github.com/Kirby980/agent/mcp"
	"github.com/Kirby980/agent/patterns"
	"github.com/Kirby980/agent/router"
	"github.com/Kirby980/agent/skill"
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

	if err := runOnce(ctx, cfg); err != nil {
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
	// 按优先级从低到高排列：系统内置 -> 用户全局 -> 项目本地
	// 后加载的同名技能会覆盖先加载的（即本地开发优先）
	skillSearchPaths := []string{
		"~/.gemini/antigravity-cli/builtin/skills", // Gemini/Antigravity 系统内置
		"~/.codex/skills/.system",                  // Codex 系统内置
		"~/.codex/skills",                          // Codex 用户自定义
		"~/.agent/skills",                          // 你的 Agent 全局技能
		"./skills",                                 // 当前项目本地技能（最高优先）
	}

	skillMap := make(map[string]skill.Skill)
	for _, path := range skillSearchPaths {
		loaded, err := skill.Load(path)
		if err != nil {
			continue // 很多目录可能不存在，静默跳过即可
		}
		for _, s := range loaded {
			// 后加载的同名 Skill 覆盖前面的（实现项目级覆盖全局级）
			skillMap[strings.ToLower(s.Name)] = s
		}
	}

	var skills []skill.Skill
	for _, s := range skillMap {
		skills = append(skills, s)
	}

	// 包装为单个 llm.Provider 注入 Agent
	clusterProvider := r.AsProvider("multi-provider-cluster")
	fs, err := builtin.NewFileSystem("./")
	if err != nil {
		fmt.Println(err)
		return err
	}
	box := builtin.NewDockerSandbox("")
	client, err := mcp.NewStdioClient("./mcp/mcp-server")
	if err != nil {
		return err
	}
	defer client.Close()

	// 2. 完成 MCP 协议握手
	_, _ = client.Initialize(ctx)
	_ = client.Initialized(ctx)

	// 3. 将 MCP Server 中的工具拉取并桥接成 []tool.Tool
	mcpTools, err := mcp.BridgeAll(ctx, client)
	if err != nil {
		return err
	}
	bash := builtin.NewBash("./")
	mcpTools = append(mcpTools, fs.ListDirTool(), fs.ReadFileTool(), bash.Tool(), box.CodeRunnerTool())
	if len(skills) > 0 {
		mcpTools = append(mcpTools, skill.NewSkillTool(skills))
	}
	// 3. 构造包含代码感知与技能清单的 System Prompt
	systemPrompt := "你是一个具备代码探索与终端执行能力的 AI 命令行助手。当前工作目录是本项目的根目录。你可以通过 `bash`、`list_dir` 与 `read_file` 探索代码、执行命令和测试。"
	if skillPrompt := skill.FormatSkillsPrompt(skills); skillPrompt != "" {
		systemPrompt += "\n\n" + skillPrompt
	}

	approver := agent.NewConsoleApprover()
	a := agent.New(clusterProvider, "grok-4.6", tool.NewRegistry(mcpTools...),
		agent.WithStore(agent.NewFileStore("./store"), "test"),
		agent.WithSystemPrompt(systemPrompt),
		agent.WithApprover(approver),
	)

	inputCh := make(chan string)
	outputCh := make(chan string)
	defer func() {
		close(outputCh)
		close(inputCh)
	}()
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for {
			_, ok := <-outputCh
			if !ok {
				break
			}
			fmt.Println("输入你的问题:")
			if scanner.Scan() {
				inputCh <- scanner.Text()
			} else {
				break
			}
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
					fmt.Print(ev.Text)
				case agent.EventError:
					fmt.Fprintln(os.Stderr, "\n[错误]", ev.Text)
				case agent.EventDone:
					fmt.Println("\n[完成]")
					outputCh <- ""
				}
			}
		}
	}
}

func pattern(ctx context.Context, cfg Config) error {
	c := BuildProviders(cfg)
	resp, err := patterns.PGE(ctx, c[0], "grok-4.6", `帮我生成一个golang的基本代码`, 3)
	if err != nil {
		return err
	}
	fmt.Println(resp)
	return nil
}
