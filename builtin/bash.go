package builtin

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Kirby980/agent/tool"
)

// Bash 提供本地命令执行能力
type Bash struct {
	workDir string
}

func NewBash(workDir string) *Bash {
	if workDir == "" {
		workDir = "."
	}
	return &Bash{workDir: workDir}
}

type bashArgs struct {
	Command string `json:"command" desc:"要在本地终端执行的 Shell 命令行，例如 'ls -la'、'git status' 或 'go test ./...'"`
}

// Tool 返回包装好的 bash tool.Tool
func (b *Bash) Tool() tool.Tool {
	return tool.NewTypedTool("bash", "在本地终端中执行 Shell 命令行指令。支持查看系统状态、执行代码构建测试、git 操作等。",
		func(ctx context.Context, a bashArgs) (string, error) {
			cmdStr := strings.TrimSpace(a.Command)
			if cmdStr == "" {
				return "", fmt.Errorf("执行命令行不能为空")
			}

			// 设置默认 30 秒硬超时，防止阻塞挂起
			execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			cmd := exec.CommandContext(execCtx, "bash", "-c", cmdStr)
			cmd.Dir = b.workDir

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			if execCtx.Err() == context.DeadlineExceeded {
				return "", fmt.Errorf("命令执行超时（超过 30 秒），已强制终止")
			}

			out := stdout.String()
			errOut := stderr.String()

			// 限制返回输出长度，防止大日志把 Context Window 挤爆
			const maxOut = 30 * 1024
			if len(out) > maxOut {
				out = out[:maxOut] + "\n[...标准输出过长，已自动截断...]"
			}
			if len(errOut) > maxOut {
				errOut = errOut[:maxOut] + "\n[...错误输出过长，已自动截断...]"
			}

			if err != nil {
				return fmt.Sprintf("命令执行非零退出码 (%v):\nSTDOUT:\n%s\nSTDERR:\n%s", err, out, errOut), nil
			}
			if strings.TrimSpace(out) == "" && strings.TrimSpace(errOut) == "" {
				return "[命令执行成功，无输出内容]", nil
			}
			if strings.TrimSpace(errOut) != "" && strings.TrimSpace(out) == "" {
				return errOut, nil
			}
			if strings.TrimSpace(errOut) != "" {
				return fmt.Sprintf("%s\nSTDERR:\n%s", out, errOut), nil
			}
			return out, nil
		})
}
