package patterns

import (
	"context"
	"fmt"
	"strings"

	"github.com/Kirby980/agent/llm"
)

// Route 表示一条具体的意图路由规则与处理逻辑。
type Route struct {
	Name        string
	Description string
	Handle      func(ctx context.Context, input string) (string, error)
}

// Router 是 Route 的别名，保持向下兼容。
type Router = Route

// IntentRouter 根据大模型意图分类结果将输入分发给对应 Route。
type IntentRouter struct {
	Provider llm.Provider
	Model    string
	Routes   []Route
	Fallback func(ctx context.Context, input string) (string, error) // 备选/兜底
}

// InterRouter 兼容旧名称别名。
type InterRouter = IntentRouter

type routeChoice struct {
	Route string `json:"route"`
}

func (r *IntentRouter) Dispatch(ctx context.Context, input string) (string, error) {
	name, err := r.classify(ctx, input)
	if err == nil {
		for _, rt := range r.Routes {
			if rt.Name == name {
				return rt.Handle(ctx, input)
			}
		}
	}
	// 分类出错或没匹配上：走兜底（这本身就是一种优雅降级）
	if r.Fallback != nil {
		return r.Fallback(ctx, input)
	}
	return "", fmt.Errorf("无法路由，且未配置兜底（意图=%q, err=%v）", name, err)
}

func (r *IntentRouter) classify(ctx context.Context, input string) (string, error) {
	var b strings.Builder
	for _, rt := range r.Routes {
		fmt.Fprintf(&b, "- %s: %s\n", rt.Name, rt.Description)
	}
	system := "你是意图分类器。从下列类别中选出最匹配用户输入的一个，" +
		"严格只输出 JSON：{\"route\": \"类别名\"}。\n类别：\n" + b.String()

	out, err := complete(ctx, r.Provider, r.Model, system, input)
	if err != nil {
		return "", err
	}
	choice, err := llm.ParseInto[routeChoice](cleanJSON(out))
	if err != nil {
		return "", fmt.Errorf("分类输出无法解析: %w", err)
	}
	return choice.Route, nil
}
