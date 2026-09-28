package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chzyer/readline"
)

// Approver 权限与安全审批器接口
type Approver interface {
	Approve(ctx context.Context, toolName string, args string) (bool, error)
}

// ConsoleApprover 命令行交互式安全审批器（仿照 Codex / AGY / Claude Code）
type ConsoleApprover struct {
	mu                  sync.Mutex
	alwaysAllowTools    map[string]bool // 记录当前会话中用户选择“总是允许”的工具
	alwaysAllowCommands map[string]bool // 记录当前会话中用户选择“总是允许”的具体命令行
	safeTools           map[string]bool // 免确认的只读/安全工具白名单
}

// NewConsoleApprover 创建默认的命令行审批器
func NewConsoleApprover() *ConsoleApprover {
	return &ConsoleApprover{
		alwaysAllowTools:    make(map[string]bool),
		alwaysAllowCommands: make(map[string]bool),
		safeTools: map[string]bool{
			"read_file":             true,
			"list_dir":              true,
			"get_time":              true,
			"read_skill":            true,
			"read_memory":           true,
			"search_knowledge_base": true,
			"delegate_subagent":     true,
		},
	}
}

// SetSafeTool 注册免审批的安全工具
func (c *ConsoleApprover) SetSafeTool(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.safeTools[name] = true
}

// SafeShellPrefixes 只读/免确认的安全命令前缀白名单
var SafeShellPrefixes = []string{
	"ls", "pwd", "dir", "cat ", "head ", "tail ", "grep ", "rg ", "find ", "which ", "echo ", "file ", "wc ",
	"git status", "git diff", "git log", "git show", "git branch",
	"go version", "go env", "go list", "go test",
	"python --version", "node -v", "npm -v",
}

// DangerousShellPatterns 高危/破坏性命令特征黑名单
var DangerousShellPatterns = []string{
	"rm ", "rm\t", "rm -", "rmdir", "unlink",
	"mv ", "mv\t",
	"sudo ", "su ",
	"chmod ", "chown ",
	"kill ", "pkill ", "killall ",
	"dd ", "mkfs", "reboot", "shutdown",
	"git reset --hard", "git clean", "git push -f", "git push --force",
}

// IsSafeShellCommand 仿照 Codex 语法解析：对输入的命令行做深度分级
func IsSafeShellCommand(fullCmd string) (isSafe bool, reason string) {
	trimmed := strings.TrimSpace(fullCmd)
	if trimmed == "" {
		return true, ""
	}

	// 1. 拆分链式连接符与管道符：&&, ||, ;, |
	// 防止 "ls && rm -rf ." 这种恶意组合绕过检测
	subCmds := splitShellTokens(trimmed)

	for _, sub := range subCmds {
		sub = strings.TrimSpace(sub)
		if sub == "" {
			continue
		}

		// 检查是否有重定向写入操作（例如 > file 或 >> file）
		if strings.Contains(sub, ">") {
			return false, "包含文件输出重定向写入操作 (>)"
		}

		// 检查高危命令黑名单
		for _, pattern := range DangerousShellPatterns {
			if strings.HasPrefix(sub, pattern) || strings.Contains(sub, " "+pattern) {
				return false, fmt.Sprintf("检测到高危或破坏性命令: %q", strings.TrimSpace(pattern))
			}
		}

		// 检查是否符合安全白名单
		isSubSafe := false
		for _, prefix := range SafeShellPrefixes {
			if sub == prefix || strings.HasPrefix(sub, prefix) {
				isSubSafe = true
				break
			}
		}

		if !isSubSafe {
			return false, fmt.Sprintf("未知或非只读命令: %q", sub)
		}
	}

	return true, ""
}

// splitShellTokens 将复杂的 shell 命令拆分为独立子命令
func splitShellTokens(cmd string) []string {
	replacer := strings.NewReplacer("&&", ";", "||", ";", "|", ";")
	standardized := replacer.Replace(cmd)
	return strings.Split(standardized, ";")
}

// Approve 拦截危险工具与未知命令，向用户终端请求确认
func (c *ConsoleApprover) Approve(ctx context.Context, toolName string, args string) (bool, error) {
	// 1. 如果是终端执行工具（bash / run_command / exec），深入到命令参数级分析
	if toolName == "bash" || toolName == "run_command" || toolName == "exec" {
		return c.approveBash(ctx, toolName, args)
	}

	// 2. 普通工具级检查
	c.mu.Lock()
	if c.safeTools[toolName] || c.alwaysAllowTools[toolName] {
		c.mu.Unlock()
		return true, nil
	}
	c.mu.Unlock()

	cleanArgs := strings.TrimSpace(args)
	if len(cleanArgs) > 300 {
		cleanArgs = cleanArgs[:300] + "...(截断)"
	}

	fmt.Printf("\n⚠️  \033[33;1m[工具安全审批]\033[0m Agent 尝试调用具有潜在风险或未知的工具:\n")
	fmt.Printf("   ├─ 工具名称: \033[36m%s\033[0m\n", toolName)
	fmt.Printf("   └─ 调用参数: \033[90m%s\033[0m\n\n", cleanArgs)

	options := []string{
		"允许本次执行 (Allow once)",
		fmt.Sprintf("本次会话总是允许该工具 (%s)", toolName),
		"拒绝执行 (Reject)",
	}

	choice, err := selectApprovalMenu(options, 0)
	if err != nil {
		return false, err
	}

	switch choice {
	case 0:
		fmt.Printf("   👉 审批结果: \033[32m✔ 允许本次执行\033[0m\n\n")
		return true, nil
	case 1:
		c.mu.Lock()
		c.alwaysAllowTools[toolName] = true
		c.mu.Unlock()
		fmt.Printf("   👉 审批结果: \033[32m✔ 本次会话已总是允许该工具 (%s)\033[0m\n\n", toolName)
		return true, nil
	default:
		fmt.Printf("   👉 审批结果: \033[31m✖ 已拒绝执行\033[0m\n\n")
		return false, nil
	}
}

func (c *ConsoleApprover) approveBash(ctx context.Context, toolName string, args string) (bool, error) {
	var cmdObj struct {
		Command string `json:"command"`
		Cmd     string `json:"cmd"`
	}
	_ = json.Unmarshal([]byte(args), &cmdObj)
	rawCmd := strings.TrimSpace(cmdObj.Command)
	if rawCmd == "" {
		rawCmd = strings.TrimSpace(cmdObj.Cmd)
	}
	if rawCmd == "" {
		rawCmd = strings.TrimSpace(args)
	}

	c.mu.Lock()
	if c.alwaysAllowTools[toolName] || c.alwaysAllowCommands[rawCmd] {
		c.mu.Unlock()
		return true, nil
	}
	c.mu.Unlock()

	// 核心：命令级安全分级
	isSafe, reason := IsSafeShellCommand(rawCmd)
	if isSafe {
		// 属于只读安全命令（如 ls, pwd, git status, git diff, go test 等），直接放行！
		return true, nil
	}

	// 发现危险命令（如 rm）或未知命令，暂停并向用户弹窗询问
	fmt.Printf("\n🚨 \033[31;1m[Shell 命令安全审批]\033[0m 拦截到非只读或高危操作:\n")
	fmt.Printf("   ├─ 拦截原因: \033[33m%s\033[0m\n", reason)
	fmt.Printf("   └─ 目标命令: \033[36m%s\033[0m\n\n", rawCmd)

	options := []string{
		"允许本次执行该命令 (Allow once)",
		fmt.Sprintf("本次会话始终允许此命令 (%s)", rawCmd),
		"拒绝执行 (Reject)",
	}

	choice, err := selectApprovalMenu(options, 0)
	if err != nil {
		return false, err
	}

	switch choice {
	case 0:
		fmt.Printf("   👉 审批结果: \033[32m✔ 允许本次执行\033[0m\n\n")
		return true, nil
	case 1:
		c.mu.Lock()
		c.alwaysAllowCommands[rawCmd] = true
		c.mu.Unlock()
		fmt.Printf("   👉 审批结果: \033[32m✔ 本次会话已始终允许此命令\033[0m\n\n")
		return true, nil
	default:
		fmt.Printf("   👉 审批结果: \033[31m✖ 已拒绝执行\033[0m\n\n")
		return false, nil
	}
}

// selectApprovalMenu 交互式终端上下键选择菜单（对标 Claude Code / AGY / Codex）
func selectApprovalMenu(options []string, defaultIdx int) (int, error) {
	fd := int(os.Stdin.Fd())
	oldState, err := readline.MakeRaw(fd)
	if err != nil {
		// 非交互式终端环境（测试或管道重定向），降级为标准输入行读取
		return selectApprovalFallback(options, defaultIdx)
	}
	defer func() {
		_ = readline.Restore(fd, oldState)
	}()

	selected := defaultIdx
	if selected < 0 || selected >= len(options) {
		selected = 0
	}

	reader := bufio.NewReader(os.Stdin)

	// 隐藏光标
	fmt.Print("\033[?25l")
	defer fmt.Print("\033[?25h")

	renderMenu := func(first bool) {
		if !first {
			fmt.Printf("\033[%dA", len(options))
		}
		for i, opt := range options {
			fmt.Print("\r\033[K")
			if i == selected {
				if i == len(options)-1 {
					fmt.Printf("   \033[31;1m❯ %s\033[0m\r\n", opt)
				} else {
					fmt.Printf("   \033[32;1m❯ %s\033[0m\r\n", opt)
				}
			} else {
				fmt.Printf("     \033[90m%s\033[0m\r\n", opt)
			}
		}
	}

	renderMenu(true)

	for {
		b, err := reader.ReadByte()
		if err != nil {
			clearMenu(len(options))
			return defaultIdx, err
		}

		switch b {
		case 3: // Ctrl+C: 取消并拒绝
			clearMenu(len(options))
			return len(options) - 1, nil

		case 13, 10: // Enter (CR / LF)
			clearMenu(len(options))
			return selected, nil

		case 27: // ESC 或方向键序列
			if reader.Buffered() == 0 {
				time.Sleep(15 * time.Millisecond)
			}
			if reader.Buffered() > 0 {
				b2, err := reader.ReadByte()
				if err == nil && b2 == '[' {
					b3, err := reader.ReadByte()
					if err == nil {
						switch b3 {
						case 'A': // Up Arrow
							if selected > 0 {
								selected--
								renderMenu(false)
							}
						case 'B': // Down Arrow
							if selected < len(options)-1 {
								selected++
								renderMenu(false)
							}
						}
					}
				}
			} else {
				// 单独按 Esc：取消/拒绝
				clearMenu(len(options))
				return len(options) - 1, nil
			}

		case 'k', 'K': // vim 键位向上
			if selected > 0 {
				selected--
				renderMenu(false)
			}

		case 'j', 'J': // vim 键位向下
			if selected < len(options)-1 {
				selected++
				renderMenu(false)
			}

		case '1', 'y', 'Y':
			clearMenu(len(options))
			return 0, nil

		case '2', 'a', 'A':
			if len(options) > 1 {
				clearMenu(len(options))
				return 1, nil
			}

		case '3', 'n', 'N', 'q', 'Q':
			clearMenu(len(options))
			return len(options) - 1, nil
		}
	}
}

// clearMenu 清除选项渲染行，以便回显紧凑的一行决策结果
func clearMenu(n int) {
	fmt.Printf("\033[%dA", n)
	for i := 0; i < n; i++ {
		fmt.Print("\r\033[K\n")
	}
	fmt.Printf("\033[%dA", n)
}

// selectApprovalFallback 非 TTY 环境下的降级文本提示
func selectApprovalFallback(options []string, defaultIdx int) (int, error) {
	fmt.Print("   👉 请选择: [1] 允许本次 / [2] 会话始终允许 / [3] 拒绝 (默认 1): ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return defaultIdx, err
	}
	ans := strings.ToLower(strings.TrimSpace(input))
	switch ans {
	case "1", "y", "yes", "":
		return 0, nil
	case "2", "a", "always":
		return 1, nil
	case "3", "n", "no", "q":
		return len(options) - 1, nil
	default:
		return defaultIdx, nil
	}
}
