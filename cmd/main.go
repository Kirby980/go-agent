package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/chzyer/readline"

	"github.com/Kirby980/agent/agent"
	"github.com/Kirby980/agent/builtin"
	"github.com/Kirby980/agent/llm"
	"github.com/Kirby980/agent/mas"
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

	// 3. 构建基础工具箱 (Tools)
	baseRegistry, err := buildToolRegistry(ctx, skills, cfg.RAG)
	if err != nil {
		return fmt.Errorf("构建工具箱失败: %w", err)
	}

	// 4. 会话与模型状态管理
	storeDir := "./store"
	_ = os.MkdirAll(storeDir, 0755)

	currentModel := cfg.Model
	currentSession := "default"
	approver := agent.NewConsoleApprover()
	sessionAccumulator := &llm.Accumulator{}

	// 注册 delegate_subagent 动态子智能体委派工具（供主 Agent 分治复杂任务）
	subagentTool := buildSubagentTool(clusterProvider, func() string { return currentModel }, baseRegistry, approver)
	mainRegistry := tool.NewRegistry(append(baseRegistry.All(), subagentTool)...)

	// 5. 构造 System Prompt
	systemPrompt := "你是一个具备代码探索、重构编辑与终端执行能力的专业 AI 编程助手。当前工作目录是本项目的根目录。\n" +
		"你可以通过 `bash`、`list_dir`、`read_file`、`write_file`、`replace_content` 探索并修改代码。\n" +
		"当你面对复杂、繁重或多步骤任务时（如大范围代码调研、重构整个模块、编写多文件测试），强烈建议调用 `delegate_subagent` 工具派发给专门的子智能体（如 researcher 负责调研、coder 负责实现、reviewer 负责审查）。子智能体拥有隔离的上下文，能避免你的主上下文被海量中间检索输出污染。"
	if skillPrompt := skill.FormatSkillsPrompt(skills); skillPrompt != "" {
		systemPrompt += "\n\n" + skillPrompt
	}

	// 创建 Agent 实例的闭包辅助函数
	createAgentInstance := func(sessionID, model string) *agent.Agent {
		return agent.New(
			clusterProvider,
			model,
			mainRegistry,
			agent.WithStore(agent.NewFileStore(storeDir), sessionID),
			agent.WithSystemPrompt(systemPrompt),
			agent.WithApprover(approver),
		)
	}

	currentAgent := createAgentInstance(currentSession, currentModel)

	// 6. 初始化交互式行编辑器（支持实时 '/' 下拉建议、上下键切换高亮、历史漫游与 Shell 直通）
	homeDir, _ := os.UserHomeDir()
	historyFile := filepath.Join(homeDir, ".agent_history")
	promptStr := fmt.Sprintf("\033[36magent\033[0m (\033[33m%s\033[0m) > ", currentSession)
	ed := NewLineEditor(promptStr, historyFile, defaultSlashCommands)

	// 7. 打印启动 Banner
	printWelcomeBanner(currentSession, currentModel, configPath, len(skills), len(providers))

	// 监听中断信号（用于打断大模型流式输出，而非直接退出整个程序）
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT)
	defer signal.Stop(sigChan)

	for {
		line, err := ed.Readline()
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

		// === 1. 本地 Shell 命令直通 (!<cmd>，类似 Claude Code) ===
		if strings.HasPrefix(line, "!") {
			cmdStr := strings.TrimSpace(strings.TrimPrefix(line, "!"))
			if cmdStr == "" {
				fmt.Println("用法: !<shell命令> (例如: !git status, !ls -la, !pwd)")
				continue
			}
			runLocalShellCommand(cmdStr)
			continue
		}

		// === 2. 斜杠指令解析 (Slash Commands) ===
		if strings.HasPrefix(line, "/") {
			parts := strings.Fields(line)
			cmd := strings.ToLower(parts[0])

			// 检查是否属于系统已知指令
			isKnownCmd := false
			switch cmd {
			case "/help", "/?", "/new", "/resume", "/sessions", "/model", "/config",
				"/stats", "/tokens", "/cost", "/clear", "/skills", "/team", "/exit", "/quit":
				isKnownCmd = true
			}

			// 如果不是已知指令，但第一项包含多级路径（如 /home/... 或 /a/b）或者本地磁盘确实存在该路径，
			// 则判定这是用户提问中的文件/目录绝对路径，直接放行给 Agent，避免误拦截为未知指令。
			isPath := strings.Contains(parts[0][1:], "/")
			if !isPath {
				if _, err := os.Stat(parts[0]); err == nil || !os.IsNotExist(err) {
					isPath = true
				}
			}

			if isKnownCmd || !isPath {
				switch cmd {
				case "/help", "/?":
					printHelp()
					continue

				case "/new":
					newSess := fmt.Sprintf("session_%s", time.Now().Format("20060102_150405"))
					if len(parts) > 1 {
						newSess = parts[1]
					}
					currentSession = newSess
					currentAgent = createAgentInstance(currentSession, currentModel)
					ed.SetPrompt(fmt.Sprintf("\033[36magent\033[0m (\033[33m%s\033[0m) > ", currentSession))
					fmt.Printf("✓ 已开启全新会话: \033[33m%s\033[0m (上下文已重置)\n\n", currentSession)
					continue

				case "/resume":
					if len(parts) < 2 {
						fmt.Println("用法: /resume <会话名称>")
						listSessions(storeDir, currentSession)
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
					ed.SetPrompt(fmt.Sprintf("\033[36magent\033[0m (\033[33m%s\033[0m) > ", currentSession))
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

				case "/team":
					if len(parts) < 2 {
						fmt.Println("用法: /team <复合任务描述>")
						fmt.Println("例如: /team 审计 mas 和 plan 模块的代码质量并编写单元测试")
						continue
					}
					taskDesc := strings.TrimSpace(strings.TrimPrefix(line, parts[0]))
					teamCtx, cancelTeam := context.WithCancel(ctx)
					teamDone := make(chan struct{})
					go func() {
						select {
						case <-sigChan:
							fmt.Println("\n\033[31m[已中断团队协作执行]\033[0m")
							cancelTeam()
						case <-teamDone:
						}
					}()
					runSupervisorTeam(teamCtx, clusterProvider, currentModel, baseRegistry, approver, taskDesc)
					cancelTeam()
					close(teamDone)
					continue

				case "/exit", "/quit":
					fmt.Println("再见！")
					return nil

				default:
					fmt.Printf("未知指令: %s，输入 /help 查看支持的指令。\n", cmd)
					continue
				}
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
	baseRegistry, err := buildToolRegistry(ctx, skills, cfg.RAG)
	if err != nil {
		return err
	}

	model := cfg.Model
	approver := agent.NewConsoleApprover()
	subagentTool := buildSubagentTool(clusterProvider, func() string { return model }, baseRegistry, approver)
	mainRegistry := tool.NewRegistry(append(baseRegistry.All(), subagentTool)...)

	systemPrompt := "你是一个具备代码探索、重构编辑与终端执行能力的专业 AI 命令行助手。当前工作目录是本项目的根目录。\n" +
		"你可以使用 bash、list_dir、read_file、write_file、replace_content 等工具探索并操作代码。\n" +
		"遇到复杂或跨模块任务时，可调用 'delegate_subagent' 工具派发给专门的子智能体分治处理。"
	ag := agent.New(
		clusterProvider,
		model,
		mainRegistry,
		agent.WithSystemPrompt(systemPrompt),
		agent.WithApprover(approver),
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
	fmt.Println("\033[1;36m│\033[0m   输入 \033[33m/\033[0m 唤起指令菜单, \033[33m!<命令>\033[0m 执行 Shell, \033[33m/exit\033[0m 或 Ctrl+D 退出 \033[1;36m│\033[0m")
	fmt.Println("\033[1;36m╰─────────────────────────────────────────────────────────────╯\033[0m")
	fmt.Println()
}

func printHelp() {
	fmt.Print(`
✦ 可用指令 (Slash Commands):
  /                  打开交互式指令选择菜单 (支持 ↑/↓ 切换与 Enter 确认)
  /new [会话名]      开启一个全新会话（重置并清空上下文历史）
  /resume <会话名>   恢复历史会话及上下文记忆
  /sessions          列出本地保存的所有历史会话
  /model [模型名]    查看或动态切换模型 (如 gemini-2.5-pro, gemini-2.5-flash)
  /config            查看当前生效的配置文件路径与详细参数
  /stats             查看当前会话的累计 Token 消耗与缓存命中率
  /skills            查看当前挂载的技能清单 (SKILL.md)
  /team <任务描述>   启动 Supervisor 多智能体团队协同执行复杂工程任务
  /clear             清空屏幕
  /help              显示此帮助菜单
  exit 或 /exit      退出 CLI

✦ 多智能体分治 (Subagent):
  主 Agent 在面对复杂任务时，会自动调用 'delegate_subagent' 工具派发独立子 Agent（如
  researcher 探索代码、coder 编写实现、reviewer 审查测试）。子 Agent 上下文物理隔离，
  不污染主对话历史。

✦ 本地 Shell 直通 (类似 Claude Code):
  !<shell命令>       直接在本地执行 Shell 命令（例如: !git status, !ls -la, !go test ./...）
`)
}

// runLocalShellCommand 直接在本地宿主机执行 Shell 命令（类似 Claude Code 的 !<cmd>）
func runLocalShellCommand(cmdStr string) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd.exe", "/c", cmdStr)
	} else {
		cmd = exec.Command(shell, "-c", cmdStr)
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("\033[90m$ %s\033[0m\n", cmdStr)
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			fmt.Printf("\033[31m[命令退出码: %d]\033[0m\n\n", exitErr.ExitCode())
		} else {
			fmt.Printf("\033[31m[命令执行失败: %v]\033[0m\n\n", err)
		}
	} else {
		fmt.Println()
	}
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

// SubagentArgs 定义主 Agent 派发子任务给独立 Subagent 时的输入参数契约。
// 利用 schema.Generate 自动将 Go 结构体反射提取为大模型所需的 JSON Schema 规范。
type SubagentArgs struct {
	Role string `json:"role" desc:"子智能体的专职角色，如 'researcher'(代码探索/架构调研), 'coder'(精准编写/修改代码), 'reviewer'(代码审查/测试)"`
	Task string `json:"task" desc:"派发给该子智能体的具体子任务目标与要求细节"`
}

// buildSubagentTool 构造用于动态委派子智能体的 delegate_subagent 工具。
//
// 核心架构哲学（对标 Claude Code 与 Google AGY 的 Subagent 机制）：
// 1. 上下文物理隔离（Context Isolation）：
//    子智能体创建时不挂载主会话的持久化 Store，拥有完全独立的 State 结构体。子智能体为了完成任务
//    进行的大量文件遍历（list_dir）、全文检索（read_file/grep）产生的海量中间 Observation 不会追加到
//    主 Agent 的对话历史中，从根本上防止了“主上下文膨胀”和“注意力稀释（Lost in the middle）”。
// 2. 防递归爆炸（Anti-Recursion）：
//    子智能体仅被注入 baseReg（基础代码读写与终端工具），而不注入自身（delegate_subagent），
//    确保子 Agent 无法递归派生无限孙 Agent，避免并发与 Token 资源失控。
// 3. 结果精炼蒸馏（Distillation）：
//    子 Agent 运行至 EventDone 后，仅将浓缩总结回填为主 Agent 的工具调用结果，主 Agent 基于高度精炼的
//    汇报向最终用户提供高质量回答。
func buildSubagentTool(provider llm.Provider, getModel func() string, baseReg *tool.Registry, approver agent.Approver) tool.Tool {
	return tool.NewTypedTool[SubagentArgs](
		"delegate_subagent",
		"派发独立的子智能体（Subagent）执行专门的子任务（如深入代码调研、测试用例编写、安全审计等）。"+
			"子智能体在完全物理隔离的全新上下文中执行，不会污染主对话历史，执行完毕后仅回传精炼结论。"+
			"常用角色: 'researcher'(代码探索与架构调研), 'coder'(精准编写与重构), 'reviewer'(代码审查与测试验证)。",
		func(ctx context.Context, args SubagentArgs) (string, error) {
			role := strings.TrimSpace(args.Role)
			if role == "" {
				role = "worker"
			}
			task := strings.TrimSpace(args.Task)
			if task == "" {
				return "", fmt.Errorf("task 不能为空")
			}

			// 终端可视态横幅通知：提示用户当前主 Agent 正在派发并调起子智能体
			fmt.Printf("\n\033[1;36m┌── 🤖 [启动 Subagent: %s] ───────────────────────────────\033[0m\n", role)
			fmt.Printf("\033[1;36m│\033[0m 目标: %s\n", task)
			fmt.Printf("\033[1;36m└────────────────────────────────────────────────────────\033[0m\n")

			// 根据指派角色注入专职 System Prompt，强化其角色职能定位
			rolePrompt := fmt.Sprintf("你是一个专职的【%s】子智能体。当前工作目录是项目根目录。\n"+
				"你可以使用 bash、list_dir、read_file、write_file、replace_content 等工具探索并操作代码。\n"+
				"你的职责是专注高效地完成用户派发的任务，并在最终直接给出结论和关键代码/发现。\n"+
				"请直接使用工具进行探索，并在完成后输出结构清晰的总结报告。", role)

			// 实例化物理隔离的全新 Subagent 实例（独立生命周期、无旧历史负担、基础工具集）
			subAgent := agent.New(
				provider,
				getModel(),
				baseReg,
				agent.WithSystemPrompt(rolePrompt),
				agent.WithApprover(approver),
			)

			// 实时捕获子 Agent 的流式思考与工具调用事件，向终端缩进打标输出，提供透明的运行感知
			var subAnswer strings.Builder
			for ev := range subAgent.RunStream(ctx, task) {
				switch ev.Type {
				case agent.EventThought:
					fmt.Printf("  \033[35m[%s 思考]\033[0m %s\n", role, ev.Text)
				case agent.EventToolCall:
					fmt.Printf("  \033[34m[%s 工具]\033[0m %s(%s)\n", role, ev.Tool, ev.Args)
				case agent.EventToolResult:
					lines := strings.Split(strings.TrimSpace(ev.Text), "\n")
					snippet := lines[0]
					if len(lines) > 1 {
						snippet += fmt.Sprintf(" ... (共 %d 行)", len(lines))
					}
					fmt.Printf("  \033[32m[%s 结果]\033[0m %s\n", role, snippet)
				case agent.EventAnswerDelta:
					subAnswer.WriteString(ev.Text)
				case agent.EventDone:
					if ev.Text != "" && subAnswer.Len() == 0 {
						subAnswer.WriteString(ev.Text)
					}
				case agent.EventError:
					fmt.Fprintf(os.Stderr, "  \033[31m[%s 错误]\033[0m %s\n", role, ev.Text)
					return "", fmt.Errorf("subagent %s 失败: %s", role, ev.Text)
				}
			}

			finalRes := subAnswer.String()
			fmt.Printf("\033[32m✔ [Subagent: %s 执行完成]\033[0m\n\n", role)
			// 仅返回格式化结论，过滤掉子 Agent 内部海量的思考与原始日志
			return fmt.Sprintf("【子智能体 %s 的执行汇报】\n%s", role, finalRes), nil
		},
	)
}

// runSupervisorTeam 启动多智能体主管督导（Supervisor-Worker）模式。
//
// 架构协作流：
// 1. 角色分工：
//    - Supervisor (团队主管)：宏观决策大脑。只分析“当前做到哪一步”以及“下一步该派谁干活”，直到裁决 FINISH。
//    - researcher (调研员)：探索工程代码库、排查依赖结构、定位 Bug 隐患，输出客观调研报告。
//    - coder (研发工程师)：结合需求与调研结论，专注写代码、重构模块或编写修复方案。
//    - reviewer (质检员)：严格把关代码改动、执行自动化测试（go test）验证改动正确性。
// 2. 状态递进（Rolling Progress）：
//    每轮 Worker 执行完成后的产出都会被追加到全局 progress 缓冲区中，并在下一轮连同任务目标一起
//    提交给主管进行决策，实现多轮次多角色接力推进大工程任务。
func runSupervisorTeam(ctx context.Context, provider llm.Provider, model string, reg *tool.Registry, approver agent.Approver, task string) {
	fmt.Printf("\n\033[1;36m================ 🚀 启动多智能体团队协作 (Supervisor 模式) ================\033[0m\n")
	fmt.Printf("任务目标: \033[1m%s\033[0m\n", task)
	fmt.Printf("团队成员: \033[33mresearcher\033[0m (探索调研), \033[32mcoder\033[0m (编码重构), \033[35mreviewer\033[0m (质检验收)\n\n")

	// 1. 组建专职工人名册
	workers := map[string]*agent.Agent{
		"researcher": agent.New(
			provider,
			model,
			reg,
			agent.WithSystemPrompt("你是团队中的专职【代码探索与架构调研专家】(researcher)。当前工作目录是项目根目录。\n"+
				"你的职责是深入分析代码结构、寻找关键依赖与潜在隐患，给出客观翔实、结构清晰的调研报告。"),
			agent.WithApprover(approver),
		),
		"coder": agent.New(
			provider,
			model,
			reg,
			agent.WithSystemPrompt("你是团队中的专职【核心研发工程师】(coder)。当前工作目录是项目根目录。\n"+
				"你的职责是根据任务要求和已有调研进展，编写、重构或修复代码，保证代码正确性与工程质量。"),
			agent.WithApprover(approver),
		),
		"reviewer": agent.New(
			provider,
			model,
			reg,
			agent.WithSystemPrompt("你是团队中的专职【代码评审与质量测试员】(reviewer)。当前工作目录是项目根目录。\n"+
				"你的职责是严格审查代码改动、执行测试验证并指出缺陷。若各项指标达标且功能完整，请明确告知任务完成。"),
			agent.WithApprover(approver),
		),
	}

	// 2. 实例化主管调度器，挂载 OnStep 步进可视化回调
	sup := &mas.Supervisor{
		Provider: provider,
		Model:    model,
		Workers:  workers,
		MaxTurns: 8, // 最大调度轮数保护，防止多模型互相推诿产生无限死循环
		OnStep: func(turn int, d mas.SupervisorDecision, result string) {
			if result == "" {
				// 阶段 A：主管做出本轮裁决
				if strings.EqualFold(d.Next, "FINISH") {
					fmt.Printf("\n\033[1;32m🎯 [主管裁决] 判定全案任务已圆满达成 (FINISH)！\033[0m\n理由: %s\n", d.Reason)
				} else {
					fmt.Printf("\n\033[1;34m👑 [第 %d 轮主管指派] 成员: \033[33m%s\033[0m\033[0m\n理由: %s\n", turn+1, d.Next, d.Reason)
					fmt.Printf("   ⏳ [%s] 正在执行任务...\n", d.Next)
				}
			} else {
				// 阶段 B：被指派成员完成本轮工作
				fmt.Printf("   ✔ [%s] 完成本轮工作，交付进展\n", d.Next)
			}
		},
	}

	// 3. 运行督导主循环
	finalOutput, err := sup.Run(ctx, task)
	if err != nil {
		fmt.Printf("\n\033[31m[团队执行异常]\033[0m %v\n", err)
	}
	fmt.Printf("\n\033[1;36m================ 团队协同最终交付成果 ================\033[0m\n\n")
	fmt.Println(finalOutput)
}
