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
	Path string `json:"path" desc:"相对于知识库根目录的文件路径"`
}

func (fs *FileSystem) ReadFileTool() tool.Tool {
	return tool.NewTypedTool("read_file", "读取知识库目录下的文本文件内容",
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
