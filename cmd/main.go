package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/chzyer/readline"

	"github.com/Kirby980/agent/agent"
	"github.com/Kirby980/agent/builtin"
	"github.com/Kirby980/agent/llm"
	"github.com/Kirby980/agent/mcp"
	"github.com/Kirby980/agent/rag"
	"github.com/Kirby980/agent/router"
	"github.com/Kirby980/agent/skill"
	"github.com/Kirby980/agent/tool"
)

const version = "1.0.0"

func main() {
	// 全局上下文与优雅退出
	rootCtx, rootCancel := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer rootCancel()

	cfg, configPath, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}

	// 支持单次执行模式 (One-shot Mode，类似 claude "prompt")
	if len(os.Args) > 1 {
		arg := os.Args[1]
		if arg == "-v" || arg == "--version" {
			fmt.Printf("Go-Agent CLI v%s\n", version)
			return
		}
		if arg == "-h" || arg == "--help" {
			printHelp()
			return
		}
		// 如果传入的是一段自然语言提问，执行单次运行模式并退出
		prompt := strings.Join(os.Args[1:], " ")
		if err := runSingleTurn(rootCtx, cfg, prompt); err != nil {
			fmt.Fprintf(os.Stderr, "运行失败: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// 默认进入交互式 CLI (REPL 模式，类似 Codex / Claude Code / AGY)
	if err := runInteractiveCLI(rootCtx, cfg, configPath); err != nil {
		fmt.Fprintf(os.Stderr, "CLI 异常退出: %v\n", err)
		os.Exit(1)
	}
}

// runInteractiveCLI 启动交互式终端界面
func runInteractiveCLI(ctx context.Context, cfg Config, configPath string) error {
	// 1. 初始化 Provider 与模型路由器
	providers := BuildProviders(cfg)
	if len(providers) == 0 {
		return fmt.Errorf("未找到任何可用 Provider，请检查环境变量或配置文件: %s", configPath)
	}

	r, err := router.New(router.NewRoundRobin(), providers...)
	if err != nil {
		return fmt.Errorf("初始化模型路由器失败: %w", err)
	}
	clusterProvider := r.AsProvider("multi-provider-cluster")

	// 2. 加载技能 (Skills)
	skills := loadAllSkills()

	// 3. 构建工具箱 (Tools)
	registry, err := buildToolRegistry(ctx, skills, cfg.RAG)
	if err != nil {
		return fmt.Errorf("构建工具箱失败: %w", err)
	}

	// 4. 构造 System Prompt
	systemPrompt := "你是一个具备代码探索、重构编辑与终端执行能力的专业 AI 编程助手。当前工作目录是本项目的根目录。你可以通过 `bash`、`list_dir`、`read_file`、`write_file`、`replace_content` 探索并修改代码。"
	if skillPrompt := skill.FormatSkillsPrompt(skills); skillPrompt != "" {
		systemPrompt += "\n\n" + skillPrompt
	}

	// 5. 会话与模型状态管理
	storeDir := "./store"
	_ = os.MkdirAll(storeDir, 0755)

	currentModel := cfg.Model
	currentSession := "default"
	approver := agent.NewConsoleApprover()
	sessionAccumulator := &llm.Accumulator{}

	// 创建 Agent 实例的闭包辅助函数
	createAgentInstance := func(sessionID, model string) *agent.Agent {
		return agent.New(
			clusterProvider,
			model,
			registry,
			agent.WithStore(agent.NewFileStore(storeDir), sessionID),
			agent.WithSystemPrompt(systemPrompt),
			agent.WithApprover(approver),
		)
	}

	currentAgent := createAgentInstance(currentSession, currentModel)

	// 6. 初始化 Readline（支持上下键历史记录与流畅行编辑）
	homeDir, _ := os.UserHomeDir()
	historyFile := filepath.Join(homeDir, ".agent_history")
	rl, err := readline.NewEx(&readline.Config{
		Prompt:            fmt.Sprintf("\033[36magent\033[0m (\033[33m%s\033[0m) > ", currentSession),
		HistoryFile:       historyFile,
		InterruptPrompt:   "^C",
		EOFPrompt:         "exit",
		HistorySearchFold: true,
	})
	if err != nil {
		return fmt.Errorf("初始化终端交互组件失败: %w", err)
	}
	defer rl.Close()

	// 7. 打印启动 Banner
	printWelcomeBanner(currentSession, currentModel, configPath, len(skills), len(providers))

	// 监听中断信号（用于打断大模型流式输出，而非直接退出整个程序）
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT)
	defer signal.Stop(sigChan)

	for {
		line, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt {
				fmt.Println("\n(提示: 输入 /exit 或按 Ctrl+D 退出)")
				continue
			} else if err == io.EOF {
				fmt.Println("\n再见！")
				break
			}
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// === 斜杠指令解析 (Slash Commands) ===
		if strings.HasPrefix(line, "/") {
			parts := strings.Fields(line)
			cmd := strings.ToLower(parts[0])

			switch cmd {
			case "/help":
				printHelp()
				continue

			case "/new":
				newSess := fmt.Sprintf("session_%s", time.Now().Format("20060102_150405"))
				if len(parts) > 1 {
					newSess = parts[1]
				}
				currentSession = newSess
				currentAgent = createAgentInstance(currentSession, currentModel)
				rl.SetPrompt(fmt.Sprintf("\033[36magent\033[0m (\033[33m%s\033[0m) > ", currentSession))
				fmt.Printf("✓ 已开启全新会话: \033[33m%s\033[0m (上下文已重置)\n\n", currentSession)
				continue

			case "/resume":
				if len(parts) < 2 {
					fmt.Println("用法: /resume <会话名称>")
					continue
				}
				targetSess := parts[1]
				sessFile := filepath.Join(storeDir, targetSess+".json")
				if _, err := os.Stat(sessFile); os.IsNotExist(err) {
					fmt.Printf("未找到名为 '%s' 的会话文件 (%s)\n", targetSess, sessFile)
					continue
				}
				currentSession = targetSess
				currentAgent = createAgentInstance(currentSession, currentModel)
				rl.SetPrompt(fmt.Sprintf("\033[36magent\033[0m (\033[33m%s\033[0m) > ", currentSession))
				fmt.Printf("✓ 已恢复会话: \033[33m%s\033[0m (历史记忆已加载)\n\n", currentSession)
				continue

			case "/sessions":
				listSessions(storeDir, currentSession)
				continue

			case "/model":
				if len(parts) > 1 {
					currentModel = parts[1]
					currentAgent = createAgentInstance(currentSession, currentModel)
					fmt.Printf("✓ 已切换模型为: \033[32m%s\033[0m\n\n", currentModel)
				} else {
					fmt.Printf("当前活动模型: \033[32m%s\033[0m (使用 /model <模型名> 进行切换)\n\n", currentModel)
				}
				continue

			case "/config":
				fmt.Println("\n=== 当前生效配置 ===")
				fmt.Printf("• 配置文件路径: %s\n", configPath)
				fmt.Printf("• 当前模型: \033[32m%s\033[0m\n", currentModel)
				fmt.Printf("• API 地址: %s\n", cfg.BaseURL)
				fmt.Printf("• 供应商类型: %s (%s)\n", cfg.Supplier, cfg.Name)
				fmt.Printf("• 知识库 RAG: 已启用 (ES: %s, 索引: %s)\n", cfg.RAG.ESHost, cfg.RAG.ESIndex)
				fmt.Println("\n(提示: 直接用编辑器修改该文件后重新启动即可永久生效，也可以通过 /model 临时切换)")
				fmt.Println("===================")
				fmt.Println()
				continue

			case "/stats", "/tokens", "/cost":
				u := sessionAccumulator.Usage
				fmt.Println("\n=== 当前会话 Token 与缓存统计 ===")
				fmt.Printf("• 累计输入 Token: %d\n", u.InputTokens)
				fmt.Printf("• 缓存命中 Token: %d (平均命中率: %.1f%%)\n", u.CachedTokens, u.CacheHitRate())
				fmt.Printf("• 累计输出 Token: %d\n", u.OutputTokens)
				fmt.Printf("• 累计总计 Token: %d\n", u.Total())
				fmt.Println("=================================")
				fmt.Println()
				continue

			case "/clear":
				fmt.Print("\033[H\033[2J")
				continue

			case "/skills":
				fmt.Println("=== 当前已加载的技能 (Skills) ===")
				for i, s := range skills {
					fmt.Printf(" [%d] %s: %s\n", i+1, s.Name, s.Description)
				}
				fmt.Println("================================")
				continue

			case "/exit", "/quit":
				fmt.Println("再见！")
				return nil

			default:
				fmt.Printf("未知指令: %s，输入 /help 查看支持的指令。\n", cmd)
				continue
			}
		}

		if line == "exit" || line == "quit" {
			fmt.Println("再见！")
			break
		}

		// === 执行 Agent 交互循环 ===
		turnCtx, cancelTurn := context.WithCancel(ctx)
		turnDone := make(chan struct{})

		// 异步监听 Ctrl+C 以便在模型生成或命令执行时中断单次调用
		go func() {
			select {
			case <-sigChan:
				fmt.Println("\n\033[31m[已中断当前执行]\033[0m")
				cancelTurn()
			case <-turnDone:
			}
		}()

		runAgentTurn(turnCtx, currentAgent, line, sessionAccumulator)
		cancelTurn()
		close(turnDone)
	}

	return nil
}

// runSingleTurn 单次执行模式
func runSingleTurn(ctx context.Context, cfg Config, prompt string) error {
	providers := BuildProviders(cfg)
	if len(providers) == 0 {
		return fmt.Errorf("未找到任何可用 Provider")
	}
	r, err := router.New(router.NewRoundRobin(), providers...)
	if err != nil {
		return err
	}
	clusterProvider := r.AsProvider("multi-provider-cluster")
	skills := loadAllSkills()
	registry, err := buildToolRegistry(ctx, skills, cfg.RAG)
	if err != nil {
		return err
	}

	model := cfg.Model

	systemPrompt := "你是一个具备代码探索与终端执行能力的专业 AI 命令行助手。请根据用户指令执行操作并输出结果。"
	ag := agent.New(
		clusterProvider,
		model,
		registry,
		agent.WithSystemPrompt(systemPrompt),
		agent.WithApprover(agent.NewConsoleApprover()),
	)

	runAgentTurn(ctx, ag, prompt, nil)
	return nil
}

// runAgentTurn 处理一轮 Agent 对话与流式事件渲染
func runAgentTurn(ctx context.Context, a *agent.Agent, input string, sessionAcc *llm.Accumulator) {
	var stepUsage *llm.Usage

	for ev := range a.RunStream(ctx, input) {
		switch ev.Type {
		case agent.EventThought:
			fmt.Printf("\n\033[35m[思考]\033[0m %s\n", ev.Text)
		case agent.EventToolCall:
			fmt.Printf("\033[34m[调用工具]\033[0m %s(\033[90m%s\033[0m)\n", ev.Tool, ev.Args)
		case agent.EventToolResult:
			// 如果输出太长，折叠展示前两行
			lines := strings.Split(strings.TrimSpace(ev.Text), "\n")
			snippet := lines[0]
			if len(lines) > 1 {
				snippet += fmt.Sprintf(" ... (共 %d 行)", len(lines))
			}
			fmt.Printf("\033[32m[工具结果]\033[0m %s\n", snippet)
		case agent.EventAnswerDelta:
			fmt.Print(ev.Text)
		case agent.EventUsage:
			if ev.Usage != nil {
				stepUsage = ev.Usage
			}
		case agent.EventError:
			fmt.Fprintf(os.Stderr, "\n\033[31m[错误]\033[0m %s\n", ev.Text)
		case agent.EventDone:
			if ev.Usage != nil {
				stepUsage = ev.Usage
			}
			fmt.Println()
			if stepUsage != nil {
				if sessionAcc != nil {
					sessionAcc.Add(*stepUsage, llm.Pricing{})
				}
				fmt.Printf("\033[90m📊 [Token 统计] 输入: %d | 缓存命中: %d (命中率 %.1f%%) | 输出: %d | 累计总计: %d\033[0m\n\n",
					stepUsage.InputTokens,
					stepUsage.CachedTokens,
					stepUsage.CacheHitRate(),
					stepUsage.OutputTokens,
					stepUsage.Total(),
				)
			}
		}
	}
}

// buildToolRegistry 组装所有工具
func buildToolRegistry(ctx context.Context, skills []skill.Skill, ragCfg RAGConfig) (*tool.Registry, error) {
	var allTools []tool.Tool

	// 1. 本地代码与文件系统工具
	fs, err := builtin.NewFileSystem("./")
	if err != nil {
		return nil, err
	}
	allTools = append(allTools,
		fs.ListDirTool(),
		fs.ReadFileTool(),
		fs.WriteFileTool(),
		fs.ReplaceContentTool(),
	)

	// 2. 终端执行工具
	bash := builtin.NewBash("./")
	allTools = append(allTools, bash.Tool())

	// 3. 沙箱代码运行工具
	box := builtin.NewDockerSandbox("")
	allTools = append(allTools, box.CodeRunnerTool())

	// 4. 企业级 RAG 知识库工具 (依据配置挂载)
	if ragCfg.Enabled {
		storeType := rag.StoreType(ragCfg.StoreType)
		if storeType == "" {
			storeType = rag.StoreTypePG
		}
		store, err := rag.NewVectorStore(ctx, rag.StoreConfig{
			Type:       storeType,
			PGConn:     ragCfg.PGConn,
			ESHost:     ragCfg.ESHost,
			ESIndex:    ragCfg.ESIndex,
			ESUsername: ragCfg.ESUsername,
			ESPassword: ragCfg.ESPassword,
		})
		if err != nil {
			fmt.Printf("[RAG] 初始化向量存储失败: %v\n", err)
		} else {
			embedder := rag.NewOpenAIEmbedder(ragCfg.EmbeddingURL, ragCfg.EmbeddingKey, ragCfg.EmbeddingModel, ragCfg.EmbeddingDim)
			kbRetriever := &rag.Retriever{
				Store:    store,
				Embedder: embedder,
			}
			allTools = append(allTools, rag.SearchTool(kbRetriever))
		}
	}

	// 5. MCP 协议工具桥接（若存在本地 MCP Server）
	if mcpClient, err := mcp.NewStdioClient("./mcp/mcp-server"); err == nil {
		_, _ = mcpClient.Initialize(ctx)
		_ = mcpClient.Initialized(ctx)
		if mcpTools, err := mcp.BridgeAll(ctx, mcpClient); err == nil && len(mcpTools) > 0 {
			allTools = append(allTools, mcpTools...)
		}
	}

	// 6. 技能扩展工具
	if len(skills) > 0 {
		allTools = append(allTools, skill.NewSkillTool(skills))
	}

	return tool.NewRegistry(allTools...), nil
}

// loadAllSkills 扫描系统与用户目录下的 SKILL.md
func loadAllSkills() []skill.Skill {
	paths := []string{
		"~/.gemini/antigravity-cli/builtin/skills",
		"~/.codex/skills/.system",
		"~/.codex/skills",
		"~/.agent/skills",
		"./skills",
	}
	skillMap := make(map[string]skill.Skill)
	for _, p := range paths {
		loaded, err := skill.Load(p)
		if err != nil {
			continue
		}
		for _, s := range loaded {
			skillMap[strings.ToLower(s.Name)] = s
		}
	}
	var res []skill.Skill
	for _, s := range skillMap {
		res = append(res, s)
	}
	return res
}

func printWelcomeBanner(session, model, configPath string, skillCount, providerCount int) {
	wd, _ := os.Getwd()
	fmt.Println()
	fmt.Println("\033[1;36m╭─────────────────────────────────────────────────────────────╮\033[0m")
	fmt.Println("\033[1;36m│\033[0m   \033[1;37m✦ Go-Agent CLI (Codex / Claude Code 架构)\033[0m                 \033[1;36m│\033[0m")
	fmt.Printf("\033[1;36m│\033[0m   会话: \033[33m%-12s\033[0m | 模型: \033[32m%-16s\033[0m             \033[1;36m│\033[0m\n", session, model)
	fmt.Printf("\033[1;36m│\033[0m   配置: \033[90m%-47s\033[0m \033[1;36m│\033[0m\n", truncateStr(configPath, 47))
	fmt.Printf("\033[1;36m│\033[0m   工作目录: \033[90m%-47s\033[0m \033[1;36m│\033[0m\n", truncateStr(wd, 47))
	fmt.Printf("\033[1;36m│\033[0m   挂载技能: %-2d 个      | 可用节点: %-2d 个                      \033[1;36m│\033[0m\n", skillCount, providerCount)
	fmt.Println("\033[1;36m│\033[0m   输入 \033[33m/help\033[0m 查看指令, \033[33m/new\033[0m 新建会话, \033[33m/exit\033[0m 或 Ctrl+D 退出   \033[1;36m│\033[0m")
	fmt.Println("\033[1;36m╰─────────────────────────────────────────────────────────────╯\033[0m")
	fmt.Println()
}

func printHelp() {
	fmt.Print(`
✦ 可用指令 (Slash Commands):
  /new [会话名]      开启一个全新会话（重置并清空上下文历史）
  /resume <会话名>   恢复历史会话及上下文记忆
  /sessions          列出本地保存的所有历史会话
  /model [模型名]    查看或动态切换模型 (如 gemini-2.5-pro, gemini-2.5-flash)
  /config            查看当前生效的配置文件路径与详细参数
  /stats             查看当前会话的累计 Token 消耗与缓存命中率
  /skills            查看当前挂载的技能清单 (SKILL.md)
  /clear             清空屏幕
  /help              显示此帮助菜单
  exit 或 /exit      退出 CLI
`)
}

func listSessions(storeDir, currentSession string) {
	files, err := os.ReadDir(storeDir)
	if err != nil {
		fmt.Printf("读取会话目录失败: %v\n", err)
		return
	}
	fmt.Println("=== 本地存储的会话列表 ===")
	count := 0
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".json") {
			name := strings.TrimSuffix(f.Name(), ".json")
			marker := "  "
			if name == currentSession {
				marker = "➔ "
			}
			info, _ := f.Info()
			fmt.Printf("%s• \033[33m%-15s\033[0m (修改时间: %s, 大小: %d 字节)\n",
				marker, name, info.ModTime().Format("2006-01-02 15:04:05"), info.Size())
			count++
		}
	}
	if count == 0 {
		fmt.Println("  (暂无保存的历史会话)")
	}
	fmt.Printf("共 %d 个会话 (使用 /resume <名称> 切换)\n\n", count)
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return "..." + s[len(s)-max+3:]
}
