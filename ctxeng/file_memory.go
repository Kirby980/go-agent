package ctxeng

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Kirby980/agent/tool"
)

type FileMemory struct{ Dir string }

func (m *FileMemory) Offload(content string, threshold int) (string, error) {
	if EstimateTokens(content) < threshold {
		return content, nil
	}
	if err := os.MkdirAll(m.Dir, 0o755); err != nil {
		return "", err
	}
	id := fmt.Sprintf("mem-%d", time.Now().UnixNano())
	if err := os.WriteFile(filepath.Join(m.Dir, id+".txt"), []byte(content), 0o600); err != nil {
		return "", err
	}
	head := []rune(content)
	if len(head) > 200 {
		head = head[:200]
	}
	// 上下文里只留：引用 + 摘要。模型要全文时调 read_memory 按 id 读回。
	return fmt.Sprintf("[内容已外置 id=%s，摘要:%s…（重要提示：内容过长已由系统安全外置，请勿重复调用原工具读取相同路径！若需全文请调用 read_memory 工具按 id 读取）]", id, string(head)), nil
}

func (m *FileMemory) Read(id string) (string, error) {
	data, err := os.ReadFile(filepath.Join(m.Dir, filepath.Base(id)+".txt"))
	return string(data), err
}

// ReadMemoryArgs 定义 read_memory 工具的参数结构
type ReadMemoryArgs struct {
	ID string `json:"id" desc:"外置内容的 ID（例如 mem-xxx）"`
}

// ReadMemory read_memory 工具查看全文内容
func ReadMemory(dir string) tool.Tool {
	mem := &FileMemory{Dir: dir}
	return tool.NewTypedTool[ReadMemoryArgs](
		"read_memory",
		"当观察到摘要提示 [内容已外置 id=...] 时，调用该工具传入 id 读取全文内容",
		func(ctx context.Context, args ReadMemoryArgs) (string, error) {
			return mem.Read(args.ID)
		},
	)
}
