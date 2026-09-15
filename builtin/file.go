package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kirby980/agent/tool"
)

type FileSystem struct {
	root string // 允许访问的根目录（绝对路径）
}

func NewFileSystem(root string) (*FileSystem, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &FileSystem{root: abs}, nil
}

// safePath 把相对路径解析为根目录内的绝对路径，越界则报错。
func (fs *FileSystem) safePath(p string) (string, error) {
	clean := filepath.Clean(filepath.Join(fs.root, p))
	if clean != fs.root && !strings.HasPrefix(clean, fs.root+string(os.PathSeparator)) {
		return "", fmt.Errorf("路径越界，拒绝访问: %s", p)
	}
	return clean, nil
}

type readFileArgs struct {
	Path string `json:"path" desc:"相对于项目根目录的文件路径，例如 'cmd/main.go' 或 'go.mod'"`
}

func (fs *FileSystem) ReadFileTool() tool.Tool {
	return tool.NewTypedTool("read_file", "读取项目指定路径下的源代码或文本文件内容",
		func(ctx context.Context, a readFileArgs) (string, error) {
			path, err := fs.safePath(a.Path)
			if err != nil {
				return "", err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return "", fmt.Errorf("读取失败: %w", err)
			}
			const maxBytes = 100 * 1024
			if len(data) > maxBytes {
				data = data[:maxBytes]
			}
			return string(data), nil
		})
}

type listDirArgs struct {
	Path string `json:"path" desc:"要列出内容的目录路径，传 '.' 或空表示项目根目录，或者传子目录如 'cmd'"`
}

func (fs *FileSystem) ListDirTool() tool.Tool {
	return tool.NewTypedTool("list_dir", "列出项目指定目录下的文件和子目录结构，用于探索项目代码组织",
		func(ctx context.Context, a listDirArgs) (string, error) {
			p := strings.TrimSpace(a.Path)
			if p == "" {
				p = "."
			}
			target, err := fs.safePath(p)
			if err != nil {
				return "", err
			}
			entries, err := os.ReadDir(target)
			if err != nil {
				return "", fmt.Errorf("读取目录失败: %w", err)
			}
			var result []string
			result = append(result, fmt.Sprintf("=== 目录 %s 内容 ===", p))
			for _, e := range entries {
				prefix := "[文件]"
				if e.IsDir() {
					prefix = "[目录]"
				}
				result = append(result, fmt.Sprintf("%s %s", prefix, e.Name()))
			}
			return strings.Join(result, "\n"), nil
		})
}
