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

type DockerSandbox struct {
	image string // 如 "python:3.11-alpine"
}

func NewDockerSandbox(image string) *DockerSandbox {
	if image == "" {
		image = "python:3.11-alpine"
	}
	return &DockerSandbox{image: image}
}

type runCodeArgs struct {
	Code string `json:"code" desc:"要在隔离沙箱中执行的 Python 代码"`
}

func (ds *DockerSandbox) CodeRunnerTool() tool.Tool {
	return tool.NewTypedTool("python_sandbox", "判断操作有风险就使用沙箱在一个安全隔离环境，受限的 Python 沙箱环境中执行代码并返回输出",
		func(ctx context.Context, a runCodeArgs) (string, error) {
			if strings.TrimSpace(a.Code) == "" {
				return "", fmt.Errorf("代码内容不能为空")
			}

			// 硬超时：最多执行 10 秒，防止死循环
			execCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()

			// 核心安全参数组合：
			// 1. --network none：断网，防止外泄数据与反弹 Shell
			// 2. --memory 256m：内存上限 256MB，防止 OOM
			// 3. --cpus 1.0：限制最多用 1 个 CPU 核
			// 4. --pids-limit 64：防 Fork Bomb 炸弹
			// 5. --read-only：容器根文件系统只读
			// 6. --tmpfs /tmp:rw,size=64m：仅挂载 64MB 临时内存目录
			// 7. --cap-drop ALL：丢弃所有 Linux Capability 特权
			// 8. --user 1000:1000：以非 root 用户执行
			// 9. --rm：退出即自动销毁容器
			cmd := exec.CommandContext(execCtx, "docker", "run", "--rm", "-i",
				"--network", "none",
				"--memory", "256m",
				"--cpus", "1.0",
				"--pids-limit", "64",
				"--read-only",
				"--tmpfs", "/tmp:rw,noexec,nosuid,size=64m",
				"--cap-drop", "ALL",
				"--user", "1000:1000",
				ds.image,
				"python", "-c", a.Code,
			)

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			if execCtx.Err() == context.DeadlineExceeded {
				return "", fmt.Errorf("代码执行超时（超过 10 秒），已被沙箱强制中止")
			}

			out := stdout.String()
			errOut := stderr.String()

			// 截断输出，防止代码打印巨量文本（如 while True: print('a')）
			const maxOut = 10 * 1024
			if len(out) > maxOut {
				out = out[:maxOut] + "\n[...输出过长，已截断...]"
			}
			if len(errOut) > maxOut {
				errOut = errOut[:maxOut] + "\n[...错误信息过长，已截断...]"
			}

			if err != nil {
				return fmt.Sprintf("执行失败:\nSTDOUT:\n%s\nSTDERR:\n%s\nError: %v", out, errOut, err), nil
			}
			return fmt.Sprintf("执行成功:\n%s", out), nil
		})
}
