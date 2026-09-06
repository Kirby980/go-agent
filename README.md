# Go AI Agent Framework

一个用 Go 语言编写的高性能、生产级 AI Agent 核心框架。支持原生的 **Function Calling** 与经典 **ReAct（Reasoning + Acting）** 自愈模式、多模型统一抽象与路由容灾、DAG 任务规划并行调度，以及带有重试限流机制的网络传输层。

---

## 🌟 核心特性

- **双模式推理引擎**：
  - **Function Calling 模式**：原生利用大模型结构化 Tool Calling 能力，自动回填 `RoleTool` 结果与 `ToolCallID`。
  - **ReAct 模式**：面向不支持工具调用的模型，动态注入工具 Schema，基于 `Thought -> Action -> Observation` 文本协议运行，并内置格式解析失败自愈重试机制。
- **安全与预算控制 (Budget Guard)**：
  - 支持最大步数 (`MaxSteps`)、累计 Token 上限 (`MaxTokens`)、截止时间 (`Deadline`) 控制。
  - 自动基于动作签名（工具名 + 参数哈希）检测死循环，防范重复动作（`MaxSameAction`）。
- **会话持久化与状态恢复 (Store & State)**：
  - 内置基于 JSON 文件的 `FileStore` 持久化，支持进程内状态恢复（`memory`）与跨轮次会话恢复。
- **DAG 任务规划与拓扑并行执行 (DAG Planner)**：
  - 支持依赖图拓扑分层（`Levels`），检测循环依赖。
  - 同层任务并发调用，层间依赖同步等待；支持单点故障快速熔断取消（`context.WithCancel`）。
- **统一模型抽象与智能路由 (LLM & Router)**：
  - 统一的 `Provider` 接口，内置 OpenAI、Claude、DeepSeek、豆包等模型适配器。
  - 支持多 Provider 优先级路由（`Priority`）与公平轮询（`RoundRobin`）负载均衡，具备自动故障降级容灾能力。
  - 提供 `AsProvider` 适配器，多模型路由器可作为单一 Provider 直接无缝注入 Agent。
- **跨平台环境凭据自动识别 (Detector)**：
  - 基于 `runtime.GOOS` 跨系统（macOS / Windows / Linux）自动识别环境变量与本地客户端配置（如 Claude Desktop、本地离线模型 Ollama 等），零配置开箱即用。
- **工业级传输层 (Transport)**：
  - 具备令牌桶限流 (`Limiter`)。
  - 支持带抖动的指数退避重试 (`Exponential Backoff with Jitter`)，优先尊重服务端的 `Retry-After` 响应头。

---

## 🏗️ 架构分层

```text
┌────────────────────────────────────────────────────────┐
│                        cmd/                            │
│           CLI 入口与配置驱动 (main.go, cfg.go)            │
└──────────────────────────┬─────────────────────────────┘
                           ▼
┌────────────────────────────────────────────────────────┐
│                       agent/                           │
│   Agent 核心调度器 (RunStream, ReAct, Function Calling)   │
│   状态管理 (State)、预算控制 (Budget)、存储 (FileStore)   │
└──────────┬───────────────────────────────┬─────────────┘
           ▼                               ▼
┌──────────────────────┐       ┌─────────────────────────┐
│        plan/         │       │          tool/          │
│  DAG 拓扑分层与并行执行 │       │  工具注册表与 Schema 转换 │
└──────────────────────┘       └─────────────────────────┘
           ▼                               ▼
┌────────────────────────────────────────────────────────┐
│                       router/                          │
│               多模型优先级路由与自动降级                  │
└──────────────────────────┬─────────────────────────────┘
                           ▼
┌────────────────────────────────────────────────────────┐
│                        llm/                            │
│    统一 Provider 接口、OpenAI / Claude 协议转换、Cost     │
└──────────────────────────┬─────────────────────────────┘
                           ▼
┌────────────────────────────────────────────────────────┐
│                 internal/transport/                    │
│           HTTP 客户端、指数退避重试、限流控制 (内部私有)    │
└────────────────────────────────────────────────────────┘
```

---

## 📁 目录结构

```text
.
├── .github/
│   └── workflows/
│       └── ci.yml               # GitHub Actions CI/CD 自动化检测
├── agent/                       # [公开] Agent 核心执行引擎
│   ├── agent.go                 # Agent 核心结构、配置选项与主调度循环
│   ├── budget.go                # 运行预算与停止条件检查
│   ├── event.go                 # 流式事件定义 (Thought, ToolCall, Result 等)
│   ├── function_call.go         # 原生 Function Calling 驱动循环
│   ├── react.go                 # ReAct 循环、自愈重试、提示词模板与解析
│   ├── state.go                 # Agent 运行快照与对话历史
│   └── store.go                 # 状态持久化契约与 FileStore 实现
├── tool/                        # [公开] 工具箱体系
│   └── tool.go                  # Tool 接口与 Registry 注册表
├── llm/                         # [公开] 统一大模型抽象层
│   ├── llm.go                   # Provider 契约、Message 与 Capability 定义
│   ├── cost.go                  # Token 费用统计
│   ├── detector/                # 跨平台本地环境凭据自动探测 (macOS/Windows/Linux)
│   │   └── detector.go          # 环境变量、本地客户端配置、离线模型自动扫描
│   ├── claude/                  # Anthropic Claude 适配实现
│   │   ├── claude.go            # Claude Messages API 适配核心
│   │   └── presets.go           # 预置官方构造函数
│   └── openai/                  # OpenAI 兼容协议适配 (DeepSeek / 豆包 等)
│       ├── openai.go            # OpenAI 协议适配核心
│       └── presets.go           # 预置 DeepSeek / 豆包 / 千问等构造函数
├── plan/                        # [公开] DAG 任务规划执行器
│   ├── plan.go                  # Task 与 Plan 结构定义
│   ├── levels.go                # 基于入度的拓扑排序分层算法
│   └── execute.go               # 按拓扑层级并行执行 Task
├── prompt/                      # [公开] 动态提示词模板
│   └── prompt.go                # 基于 text/template 的动态提示词渲染
├── router/                      # [公开] 模型路由与多活降级
│   ├── router.go                # 路由器核心实现与 AsProvider 适配器
│   └── strategy.go              # 路由策略契约 (Priority、RoundRobin 轮询等)
├── cmd/                         # [示例] 命令行示例运行入口
│   ├── config.go                # 跨平台凭据融合与 Provider 构建
│   └── main.go                  # 轮询集群驱动的 Agent 示例入口
├── internal/                    # [私有] 内部实现细节 (外部项目无法 import)
│   └── transport/               # 基础设施网络层
│       ├── client.go            # HTTP 客户端核心 (执行退避重试与限流)
│       ├── config.go            # 客户端重试与限流配置
│       └── http.go              # 底层 HTTP 连接池与超时配置
└── go.mod
```

---

## 🚀 快速上手

### 1. 运行单次调用示例

配置环境变量后即可直接运行：

```bash
export supplier="openai"
export name="deepseek"
export base_url="https://api.deepseek.com/v1"
export key="your-api-key"

go run ./cmd/main.go "请解释 Go 语言中 channel 的底层结构"
```

### 2. 使用 Agent 核心进行任务流式执行

```go
package main

import (
	"context"
	"fmt"

	"github.com/Kirby980/agent/agent"
	"github.com/Kirby980/agent/llm/openai"
	"github.com/Kirby980/agent/tool"
)

func main() {
	ctx := context.Background()

	// 1. 初始化 Provider
	provider := openai.NewOpenAI("your-api-key")

	// 2. 注册工具
	registry := tool.NewRegistry(
		// 传入实现了 tool.Tool 接口的工具实例
	)

	// 3. 构建 Agent
	ag := agent.New(
		provider,
		"gpt-4o",
		registry,
		agent.WithSystemPrompt("你是一个专业的计算与检索助手。"),
		agent.WithBudget(agent.DefaultBudget()),
	)

	// 4. 流式执行并消费事件
	eventChan := ag.RunStream(ctx, "查询今天北京的天气并计算 23 * 47")
	for event := range eventChan {
		switch event.Type {
		case agent.EventThought:
			fmt.Printf("[思考] %s\n", event.Text)
		case agent.EventToolCall:
			fmt.Printf("[调用工具] %s 参数: %s\n", event.Tool, event.Args)
		case agent.EventToolResult:
			fmt.Printf("[工具返回] %s\n", event.Text)
		case agent.EventAnswerDelta:
			fmt.Print(event.Text)
		case agent.EventError:
			fmt.Printf("\n[错误] %s\n", event.Text)
		case agent.EventDone:
			fmt.Println("\n[完成]")
		}
	}
}
```

### 3. 使用 DAG 任务规划执行器

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kirby980/agent/plan"
	"github.com/Kirby980/agent/tool"
)

func main() {
	p := plan.Plan{
		Tasks: []plan.Task{
			{ID: "task_a", Tool: "fetch_data", Args: json.RawMessage(`{}`)},
			{ID: "task_b", Tool: "fetch_data", Args: json.RawMessage(`{}`)},
			// task_c 依赖 task_a 与 task_b
			{ID: "task_c", Tool: "merge_data", DependsOn: []string{"task_a", "task_b"}},
		},
	}

	// 自动拓扑分层：task_a 与 task_b 并行，之后执行 task_c
	results, err := plan.Execute(context.Background(), p, toolRegistry)
	if err != nil {
		panic(err)
	}
	fmt.Printf("执行结果: %+v\n", results)
}
```

### 4. 跨平台凭据自动探测与多模型轮询容灾集群

系统支持自动识别 macOS、Windows、Linux 下的环境变量、客户端配置（如 Claude Desktop）及本地免鉴权离线模型（如 Ollama）：

```go
package main

import (
	"context"
	"fmt"

	"github.com/Kirby980/agent/agent"
	"github.com/Kirby980/agent/llm"
	"github.com/Kirby980/agent/llm/claude"
	"github.com/Kirby980/agent/llm/detector"
	"github.com/Kirby980/agent/llm/openai"
	"github.com/Kirby980/agent/router"
	"github.com/Kirby980/agent/tool"
)

func main() {
	// 1. 跨平台自动扫描本地环境的 API Key 和 BaseURL
	creds := detector.DetectLocalCredentials()
	var providers []llm.Provider
	for _, c := range creds {
		switch c.Supplier {
		case "openai":
			providers = append(providers, openai.NewOpenAICustom(c.Name, c.APIKey, c.BaseURL))
		case "claude":
			providers = append(providers, claude.NewClaudeCustom(c.Name, c.APIKey, c.BaseURL))
		}
	}

	// 2. 创建基于 RoundRobin 轮询且支持故障降级的模型路由器
	r, err := router.New(router.NewRoundRobin(), providers...)
	if err != nil {
		panic(err)
	}

	// 3. 将路由器适配为单 Provider 注入 Agent，请求将在节点间均匀轮询，节点故障时自动转移
	clusterProvider := r.AsProvider("multi-provider-cluster")
	ag := agent.New(
		clusterProvider,
		"grok-4.6",
		tool.NewRegistry(&tool.Calculator{}, &tool.Now{}),
	)

	fmt.Printf("成功启动 Agent，已接入 %d 个可用 Provider 节点\n", len(providers))
	_ = ag
}
```

---

## 🧪 自动化测试与 CI/CD

项目配有完整的单元测试集与 GitHub Actions 持续集成工作流。

### 本地运行测试

```bash
# 运行全部测试
go test -v ./...

# 运行竞态检测与覆盖率统计
go test -v -race -cover ./...
```

### GitHub Actions CI 检验

项目根目录包含 [`.github/workflows/ci.yml`](.github/workflows/ci.yml)，在代码推送到 `main` / `master` 分支或提交 Pull Request 时会自动执行：
1. **依赖校验**：`go mod verify`
2. **静态语法检查**：`go vet ./...`
3. **功能与竞态测试**：`go test -v -race -cover ./...` 自动运行所有包的单元测试（包括 Function Calling、ReAct、会话存储、DAG 规划、工具注册、模型路由等核心算法）
4. **编译构建校验**：`go build -v ./...`
