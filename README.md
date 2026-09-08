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
├── patterns/                    # [公开] 经典 Agent 架构模式与进阶编排
│   ├── routeting.go             # IntentRouter 意图分类与动态路由
│   ├── pge.go                   # PGE (Plan-Gen-Eval) 三层代码审查流水线
│   ├── evaluation.go            # EvaluatorOptimizer 生成-评估-优化循环
│   ├── orchestrator.go          # Orchestrator-Workers 任务分治编排
│   ├── parallelization.go       # Sectioning 并行分段与协同聚合
│   └── chainstep.go             # Prompt Chaining 链式工作流与门禁校验
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

## 💡 深度思考：这个任务真的需要三 Agent 吗？

在本项目中，我们实现了经典的 **PGE (Plan-Gen-Eval)** 三层架构代码审查流（位于 [`patterns/pge.go`](patterns/pge.go)）：
1. **Planner**：大模型充当意图与维度分类器，将代码审查解构为并发安全、性能、错误处理、架构规范等具体维度；
2. **Generator**：并行调用多个 Worker 针对各维度独立审查，最终汇总为一份审查草稿；
3. **Evaluator-Optimizer**：评估模型严格质检打分，未达标时带着反馈进行多轮自愈修订，直至达标。

但作为一个追求工程实用主义与高性价比的系统，我们必须直面这个关键问题：

> **这个任务真的需要三 Agent 吗？什么情况下一个精心提示的单次调用就够了？**

### 一、三 Agent (PGE) 的真正价值与代价

#### 1. 核心价值（何时体现优势）
- **突破长上下文的“注意力稀释 (Lost in the Middle)”**：
  若要求单个模型在单次调用中同时全面兼顾“Go 内存泄漏、锁竞争、边界越界、错误处理、架构可读性”，模型注意力容易涣散，难以深挖隐蔽 Bug。分维度由独立 Worker 审查可极大提升**缺陷检出率（Recall）**。
- **引入闭环质检（Evaluator 反思兜底）**：
  单次生成不可避免存在偶发幻觉或泛泛而谈。引入独立的 Evaluator 按量化指标（如 Score ≥ 80、必须提供代码证据）质检，不达标则循环打回重改，大幅提升输出的**严谨性与确定性**。
- **异构模型分级调度（Cost & Capability Routing）**：
  三层可解耦采用不同模型（如轻量模型做 Planner 拆解，专业代码模型做并行审查，最顶级的旗舰模型做 Evaluator 把关），在质量与成本间取得最佳平衡。

#### 2. 付出的代价（固有缺点）
- **高延迟 (Latency)**：
  单次审查需要经历：`意图分类 (1次) + 维度规划 (1次) + 维度并行 (N个Task) + 报告整合 (1次) + 质检打分 (1次) + 潜在重试循环 (2~3轮)`。端到端耗时通常在 **15s ~ 45s+**，无法胜任即时交互。
- **高 Token 消耗 (Cost)**：
  代码与上下文在多个 Agent 间反复传输与汇总，Token 消耗是单次调用的 **5 ~ 10 倍**。
- **工程复杂度与脆弱性**：
  涉及多次 JSON 解析、并发同步与多轮状态流转，任一环节格式偶发异常都会增加系统的容错与调试成本。

---

### 二、什么情况下一个精心提示的单次调用就够了？

在以下常见业务场景中，**强烈建议优先使用单次精心设计的 Prompt（Single Well-Prompted Call）**：

1. **短小、聚焦的代码片段审查（< 200 行）**：
   - **典型场景**：单个函数增改、算法实现、局部 Bugfix。
   - **原因**：现代前沿大模型（如 Claude 3.5 Sonnet、GPT-4o、DeepSeek-V3/R1）在数千 Token 内具备极高密度的推理能力，单次调用足以覆盖各维度细节，三 Agent 带来的边际收益极小，反而承担巨大的延迟惩罚。
2. **交互式 / 实时体验场景**：
   - **典型场景**：IDE 插件实时审查、代码保存即时 Lint、CLI 命令行即时对话。
   - **原因**：开发者通常要求 **2 ~ 5 秒内**得到响应。单次调用配合流式传输（Streaming）能即时吐字，而 PGE 的数十秒延迟会严重打断开发心流。
3. **成本敏感与大规模批量作业**：
   - **典型场景**：整库历史代码全量扫描、每日几十万次增量 Commit 巡检。
   - **原因**：单次调用成本仅为三 Agent 的 10%~20%，在有限预算下能够覆盖倍数级的代码资产。
4. **意图明确、检查项固定的常规审查**：
   - **典型场景**：检查是否规范使用 `defer`、是否处理了所有 `error`、命名是否符合团队规范。
   - **原因**：配合明确的 Checklist 与 Few-Shot 示例，单次调用命中率极高，不需要反复评估迭代。

---

### 三、如何写好一个“足以替代三 Agent”的单次提示词？

如果你选择单次调用，可以通过结构化提示词将 PGE 的解构与反思逻辑压缩在单次推理中（结合思维链 CoT）：

```markdown
你是一个顶尖的代码审查专家。请对用户提供的代码执行严格审查：

【思考流程（Chain-of-Thought）】
在给出最终评审结论前，请在内部逐步思考以下维度：
1. 并发与资源安全：是否存在数据竞态、未解锁、goroutine 泄漏？
2. 边界与异常处理：是否存在空指针、越界切片、未捕获的 error？
3. 性能与内存开销：是否存在高频堆分配、不必要的全量遍历？

【输出规范】
请直接输出清晰的 Markdown 报告：
- 🔴 严重缺陷（Critical）：定位到具体代码行，指出隐患原理并提供修复方案
- 🟡 优化建议（Warning）：可读性、惯用写法与性能建议
- 💡 总结评分（0-100）与结论（Pass / Fail）
```

> **核心技巧**：通过强制规定 **CoT 思考流程**、内置 **Checklist 审查维度** 并约定 **结构化输出**，单次调用在中小规模任务上的表现逼近甚至匹敌复杂的多 Agent 系统。

---

### 四、架构决策矩阵与工程法则

| 考量维度 | 单次精心提示 (Single-Call) | 三 Agent 审查 (PGE) |
| :--- | :--- | :--- |
| **响应耗时** | ⚡ **秒级响应（2~5s，支持逐字流式）** | ⏳ **较慢（15~45s+，多轮往返）** |
| **Token 成本** | 💰 **极低（1x 基准）** | 💸 **高（5x ~ 10x）** |
| **工程复杂度** | 🟢 **极简（无额外状态与编排）** | 🔴 **较高（DAG/并行/多轮质检状态维护）** |
| **代码规模** | 中小型函数、局部变更（< 200 行） | 跨文件模块、复杂并发状态机、核心重构 |
| **典型场景** | IDE 实时插件、本地 CLI、高频巡检 | CI/CD 发布门禁、核心资产安全审计、离线夜报 |
| **质量兜底** | 依赖单次模型推理能力与 Prompt 设计 | 独立 Evaluator 反思打分与迭代自愈 |

> **💡 架构设计法则**：
> 1. **不要为了 Agent 而 Agent**。优先尝试**单个强模型 + 精品 Prompt（CoT + Checklist）**探寻效果天花板。
> 2. 当遇到**长代码注意力稀释、复杂多维度审查漏报率高、或业务要求硬性打分质检门禁**时，再升级为 **PGE 多 Agent 架构**。
> 3. 本项目已通过 [`IntentRouter`](patterns/routeting.go) 实现动态前置分流：输入不是代码时走普通问答，避免无谓触发沉重的审查流水线。

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
