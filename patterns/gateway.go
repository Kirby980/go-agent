package patterns

import (
	"context"

	"github.com/Kirby980/agent/agent"
	"github.com/Kirby980/agent/llm"
	"github.com/Kirby980/agent/tool"
)

// Gateway 是统一智能分流网关，通过大模型意图分类动态调度最适配的 Agent 模式：
// 1. 单 Agent (single_agent): 通用交互与工具调用（由 Agent 自身根据 Provider 能力自适应 Function Calling 或 ReAct）
// 2. 多 Agent - PGE (multi_agent_pge): 深度代码审查（Plan -> Gen -> Eval 质检与多轮优化）
// 3. 多 Agent - Orchestrator (multi_agent_orchestrator): 复杂多目标复合任务（任务拆解 -> 独立 Worker Agents 并行 -> 结果综合）
type Gateway struct {
	router *IntentRouter
}

// GatewayOption 定义网关可选配置。
type GatewayOption func(*gatewayConfig)

type gatewayConfig struct {
	maxPGERounds int
}

// WithGatewayPGERounds 设置 PGE 最大审查优化轮次。
func WithGatewayPGERounds(rounds int) GatewayOption {
	return func(c *gatewayConfig) {
		c.maxPGERounds = rounds
	}
}

// NewGateway 构造并返回一个智能分流网关。
func NewGateway(p llm.Provider, model string, tools *tool.Registry, opts ...GatewayOption) *Gateway {
	cfg := &gatewayConfig{maxPGERounds: 3}
	for _, opt := range opts {
		opt(cfg)
	}

	routes := []Route{
		{
			Name: "single_agent",
			Description: "日常通用问答、单步骤指令、或直接需要调用工具（如查时间、基础计算、读写特定文件等）的任务",
			Handle: func(ctx context.Context, input string) (string, error) {
				ag := agent.New(p, model, tools)
				return ag.Run(ctx, input)
			},
		},
		{
			Name: "multi_agent_pge",
			Description: "代码深度审查任务（需要拆分正确性、并发、错误处理等维度独立审查，并经过 Evaluator 多轮打分质检）",
			Handle: func(ctx context.Context, input string) (string, error) {
				return RunPGEReview(ctx, p, model, input, cfg.maxPGERounds)
			},
		},
		{
			Name: "multi_agent_orchestrator",
			Description: "大型复杂任务或多步骤复合任务（需要拆解成多个子任务分别并行处理，最后统一汇总综合）",
			Handle: func(ctx context.Context, input string) (string, error) {
				orch := &Orchestrator{
					Provider: p,
					Model:    model,
					NewWorker: func() *agent.Agent {
						return agent.New(p, model, tools)
					},
				}
				return orch.Run(ctx, input)
			},
		},
	}

	router := &IntentRouter{
		Provider: p,
		Model:    model,
		Routes:   routes,
		Fallback: func(ctx context.Context, input string) (string, error) {
			// 意图不明或分类异常时，安全兜底走单 Agent 执行
			ag := agent.New(p, model, tools)
			return ag.Run(ctx, input)
		},
	}

	return &Gateway{router: router}
}

// Execute 接收用户输入，由大模型判断后分发给最适合的 Agent 模式执行。
func (g *Gateway) Execute(ctx context.Context, input string) (string, error) {
	res, _, err := g.router.DispatchWithRoute(ctx, input)
	return res, err
}

// ExecuteWithRoute 接收用户输入并返回执行结果以及选中的路由模式名称。
func (g *Gateway) ExecuteWithRoute(ctx context.Context, input string) (string, string, error) {
	return g.router.DispatchWithRoute(ctx, input)
}
