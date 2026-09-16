# Go AI Agent Framework

一个用 Go 语言编写的高性能、生产级 AI Agent 核心框架。支持原生的 **Function Calling** 与经典 **ReAct（Reasoning + Acting）** 自愈模式、企业级 **双路混合 RAG（基于自研 go-es 驱动）**、**本地代码库自主阅读与终端工具（内置 Human-in-the-Loop 安全拦截）**、多模型统一抽象与路由容灾、DAG 任务规划并行调度，以及 Model Context Protocol (MCP) 与 Agent Skills 扩展协议。

---

## 🌟 核心特性

- **双模式推理引擎**：
  - **Function Calling 模式**：原生利用大模型结构化 Tool Calling 能力，自动回填 `RoleTool` 结果与 `ToolCallID`。
  - **ReAct 模式**：面向不支持工具调用的模型，动态注入工具 Schema，基于 `Thought -> Action -> Observation` 文本协议运行，并内置格式解析失败自愈重试机制。
- **企业级双路混合检索 RAG（默认基于 `Kirby980/go-es`）**：
  - **通用 VectorStore 抽象**：面向接口解耦，支持工厂配置灵活切换（默认 ES，保留 PostgreSQL / pgvector）。
  - **自研 ES 客户端深度集成**：接入 [`github.com/Kirby980/go-es`](https://github.com/Kirby980/go-es)，利用 `BulkBuilder` 零反射流式批量写入切片文本与稠密向量，通过 `SearchBuilder.KNN` 运行原生 ES 8.x/9.x 向量检索。
  - **双路召回 + RRF 融合**：KNN 语义向量检索 + BM25 关键词倒排检索无缝融合，使用 RRF（倒数排名融合）算法智能排序，并支持二次精排（Reranker）。
  - **知识库工具化 (`rag.SearchTool`)**：一键将企业文档检索管线挂载为 Agent 工具。
- **本地代码库感知与终端执行 (Claude Code / Codex 风格)**：
  - **代码库阅读与定位**：内置 `read_file`、`list_dir`，大模型像真实程序员一样自主探索仓库结构、阅读上下文。
  - **终端命令执行 (`bash_tool`)**：支持执行 shell 命令，内置 30 秒超时控制与 30KB 输出防爆截断保护。
  - **人类在环安全审计 (Human-in-the-Loop Approver)**：精准识别只读安全命令（`ls`, `pwd`, `git status` 等自动白名单放行）与破坏性命令（`rm`, `mv`, `sudo`, 重定向写入 `>` 等触发终端交互式 `[y/a/n]` 确认）。
- **扩展生态与协议互联**：
  - **Agent Skills**：遵循标准 `SKILL.md` 规范，实现技能元数据自动扫描发现与指令增强。
  - **MCP 协议支持**：支持 Model Context Protocol (MCP)，一键挂载外部标准工具与远程服务。
- **安全与预算控制 (Budget Guard)**：
  - 支持最大步数 (`MaxSteps`)、累计 Token 上限 (`MaxTokens`)、截止时间 (`Deadline`) 控制。
  - 自动基于动作签名（工具名 + 参数哈希）检测死循环，防范重复动作（`MaxSameAction`）。
- **DAG 任务规划与拓扑并行执行 (DAG Planner)**：
  - 支持依赖图拓扑分层（`Levels`），检测循环依赖。
  - 同层任务并发调用，层间依赖同步等待；支持单点故障快速熔断取消（`context.WithCancel`）。
- **统一模型抽象与智能路由 (LLM & Router)**：
  - 统一的 `Provider` 接口，内置 OpenAI、Claude、DeepSeek、豆包等模型适配器。
  - 支持多 Provider 优先级路由（`Priority`）与公平轮询（`RoundRobin`）负载均衡，具备自动故障降级容灾能力。
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
│         CLI 交互入口、工具注册与安全拦截装配 (main.go)      │
└──────────────────────────┬─────────────────────────────┘
                           ▼
┌────────────────────────────────────────────────────────┐
│                       agent/                           │
│   Agent 核心调度器 (RunStream, ReAct, Function Calling)   │
│   人类在环命令审批 (Approver)、预算控制 (Budget)、存储 (Store)│
└──────────┬───────────────────┬─────────────────────────┘
           │                   │
           ▼                   ▼
┌──────────────────────┐ ┌──────────────────────────────────────┐
│        plan/         │ │                tool/                 │
│  DAG 拓扑分层与并行执行 │ │         工具注册表与 Schema 转换       │
└──────────────────────┘ └──────────┬──────────────┬────────────┘
                                    │              │
           ┌────────────────────────┘              └────────────────────────┐
           ▼                                                                ▼
┌──────────────────────────────────────┐        ┌──────────────────────────────────────┐
│              builtin/                │        │                 rag/                 │
│  代码阅读 (file)、终端执行 (bash)、    │        │  企业级 RAG 检索管线                  │
│  NL2SQL、沙箱隔离环境                  │        │  - VectorStore 通用接口 (ES 默认 / PG)│
└──────────────────────────────────────┘        │  - 基于 Kirby980/go-es 原生 KNN+BM25 │
                                                │  - RRF 倒数排名融合 + Reranker 精排   │
                                                └──────────────────────────────────────┘
                                                                            │
                                                                            ▼
┌───────────────────────────────────────────────────────────────────────────────────────┐
│                                       router/                                         │
│                              多模型优先级路由与自动降级                                   │
└──────────────────────────────────────────┬────────────────────────────────────────────┘
                                           ▼
┌───────────────────────────────────────────────────────────────────────────────────────┐
│                                        llm/                                           │
│                 统一 Provider 接口、OpenAI / Claude 适配、凭据自动探测 (detector)          │
└──────────────────────────────────────────┬────────────────────────────────────────────┘
                                           ▼
┌───────────────────────────────────────────────────────────────────────────────────────┐
│                                 internal/transport/                                   │
│                     HTTP 客户端、指数退避重试、限流控制 (内部私有)                          │
└───────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 📁 目录结构

```text
.
├── agent/                       # [核心] Agent 执行引擎与调度中心
│   ├── agent.go                 # Agent 结构体定义、配置项与主调度循环
│   ├── approver.go              # 人类在环 (HITL) 命令审计与交互式终端放行
│   ├── budget.go                # 运行预算控制 (Token 上限、步数、死循环检测)
│   ├── event.go                 # 流式事件类型 (Thought, ToolCall, Result 等)
│   ├── function_call.go         # 原生 Function Calling 驱动执行循环
│   ├── react.go                 # ReAct 自愈提示词与解析执行循环
│   ├── state.go                 # 运行状态快照与上下文历史
│   └── store.go                 # 会话持久化接口与 FileStore 实现
├── builtin/                     # [内置工具] 面向开发者的系统级工具集
│   ├── bash.go                  # 终端命令执行工具 (超时中断、输出截断防护)
│   ├── file.go                  # 代码库阅读 (read_file) 与目录巡检 (list_dir)
│   ├── nl2sql.go                # 自然语言转 SQL 工具与动态元数据注入
│   └── sandbox.go               # 沙箱命令隔离环境
├── rag/                         # [RAG] 企业级知识库检索与向量存储
│   ├── store.go                 # 通用 VectorStore 接口定义与 NewVectorStore 工厂
│   ├── es.go                    # 基于 Kirby980/go-es 的高性能 ES 驱动 (KNN + BM25)
│   ├── pgx.go                   # 基于 pgx + pgvector 的 PostgreSQL 驱动
│   ├── rerank.go                # Retriever 检索编排器 (双路召回 + 重排精排)
│   ├── rrf.go                   # RRF (Reciprocal Rank Fusion) 倒数排名融合算法
│   ├── chuck.go                 # 递归分块器 (RecursiveChunker，中文友好带 Overlap)
│   ├── embedding.go             # Embedder 稠密向量接口定义
│   ├── openai.go                # OpenAI / LiteLLM 兼容的向量化客户端
│   └── tool.go                  # 知识库检索 Agent 工具封装 (SearchTool)
├── tool/                        # [工具契约]
│   └── tool.go                  # Tool 接口、强类型工具转换与 Registry 注册表
├── skill/                       # [技能] Agent Skills 体系
│   └── skill.go                 # 遵循标准规范的 SKILL.md 自动扫描与挂载
├── mcp/                         # [协议] Model Context Protocol 支持
│   ├── client.go                # MCP 客户端核心驱动
│   └── bridged.go               # 将 MCP Tool 映射为原生 Tool 桥接器
├── llm/                         # [模型层] 统一 Provider 抽象与多厂商协议
│   ├── llm.go                   # Provider 接口、Message 结构体定义
│   ├── cost.go                  # Token 费用统计
│   ├── detector/                # 跨平台 (macOS/Win/Linux) 本地凭据探测
│   ├── claude/                  # Anthropic Claude 适配器
│   └── openai/                  # OpenAI 兼容协议适配器 (DeepSeek, 豆包, 千问等)
├── plan/                        # [编排] DAG 任务规划执行器
│   ├── plan.go                  # Task 与 Plan 结构定义
│   ├── levels.go                # 拓扑分层算法 (入度检测与环路检查)
│   └── execute.go               # 按拓扑层级并行执行
├── patterns/                    # [模式] 经典进阶 Agent 编排流水线
│   ├── pge.go                   # PGE (Plan-Gen-Eval) 三层代码审查流水线
│   ├── routeting.go             # 意图分类与动态前置路由
│   ├── evaluation.go            # 生成-评估-优化循环
│   └── orchestrator.go          # 任务分治编排
├── router/                      # [容灾路由] 多模型优先级与负载均衡
├── cmd/                         # [命令行] CLI 交互入口
└── internal/transport/          # [网络基础设施] 重试、退避、限流
```

---

## 🚀 快速上手

### 1. 运行类 Claude Code 交互式代码助手

配置大模型凭据后即可直接启动。助手已默认挂载代码阅读、目录查看以及安全受控的终端执行工具：

```bash
export supplier="openai"
export name="gemini"
export base_url="http://localhost:4000/v1"
export key="your-api-key"

go run ./cmd/main.go
```

**交互示例**：
```text
Agent 已就绪，请输入你的指令 (输入 exit 退出):
> 帮我查看当前目录下的 go.mod 内容，并列出 rag 目录下的所有文件

[调用工具] read_file 参数: {"path":"go.mod"}
[工具返回] module github.com/Kirby980/agent ...
[调用工具] list_dir 参数: {"path":"rag"}
[工具返回] - chuck.go (file) ...
...
```

当大模型尝试执行写操作或破坏性 Shell 命令（如 `rm` 或 `git clean`）时，**Approver 安全卫士会自动拦截并提示用户确认**：
```text
[安全警告] 准备执行系统命令:
  rm -rf ./tmp_cache
是否批准执行? [y:批准 / a:本次全部批准 / n:拒绝]:
```

---

### 2. 企业级 RAG 检索管线（基于 `Kirby980/go-es`）

#### 写入与检索实战

```go
package main

import (
	"context"
	"fmt"

	"github.com/Kirby980/agent/rag"
)

func main() {
	ctx := context.Background()

	// 1. 初始化向量存储（工厂函数，默认使用 Elasticsearch）
	store, err := rag.NewVectorStore(ctx, rag.StoreConfig{
		Type:    rag.StoreTypeES,            // 可选：rag.StoreTypePG
		ESHost:  "http://localhost:9200",
		ESIndex: "enterprise_kb",
	})
	if err != nil {
		panic(err)
	}

	// 2. 批量写入文档分块与对应向量（内部采用 go-es 的 BulkBuilder 零反射极速入库）
	chunks := []string{
		"当微信支付返回 40301 错误码时，说明商户证书已失效，需重新下载并更新至网关。",
		"退款接口每日限额在商户平台后台可配置，默认单笔不超过 5 万元。",
	}
	embs := [][]float32{
		// ... 对应的 768 维向量
	}
	_ = store.Add(ctx, "payment_manual.pdf", chunks, embs)

	// 3. 构建混合检索编排器（同时做 KNN 向量召回 + BM25 倒排召回，自动 RRF 融合）
	retriever := &rag.Retriever{
		Store:    store,
		Embedder: embedder, // 你的 Embedder 实例
	}

	// 4. 将检索器一键包装为 Agent 工具
	kbTool := rag.SearchTool(retriever)
	_ = kbTool
}
```

---

### 3. DAG 任务规划执行器

支持通过有向无环图自动并行化独立任务，按依赖拓扑层级递进执行：

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kirby980/agent/plan"
)

func main() {
	p := plan.Plan{
		Tasks: []plan.Task{
			{ID: "fetch_api_doc", Tool: "read_file", Args: json.RawMessage(`{"path":"docs/api.md"}`)},
			{ID: "fetch_db_schema", Tool: "read_file", Args: json.RawMessage(`{"path":"schema.sql"}`)},
			// analyze 依赖前两项任务的结果，前两项并行执行完成后自动触发 analyze
			{ID: "analyze", Tool: "analyze_diff", DependsOn: []string{"fetch_api_doc", "fetch_db_schema"}},
		},
	}

	results, err := plan.Execute(context.Background(), p, toolRegistry)
	if err != nil {
		panic(err)
	}
	fmt.Printf("执行结果: %+v\n", results)
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

---

### 二、架构决策矩阵与工程法则

| 考量维度 | 单次精心提示 (Single-Call) | 三 Agent 审查 (PGE) |
| :--- | :--- | :--- |
| **响应耗时** | ⚡ **秒级响应（2~5s，支持逐字流式）** | ⏳ **较慢（15~45s+，多轮往返）** |
| **Token 成本** | 💰 **极低（1x 基准）** | 💸 **高（5x ~ 10x）** |
| **工程复杂度** | 🟢 **极简（无额外状态与编排）** | 🔴 **较高（DAG/并行/多轮质检状态维护）** |
| **代码规模** | 中小型函数、局部变更（< 200 行） | 跨文件模块、复杂并发状态机、核心重构 |
| **典型场景** | IDE 实时插件、本地 CLI、高频巡检 | CI/CD 发布门禁、核心资产安全审计、离线夜报 |

> **💡 架构设计法则**：
> 1. **不要为了 Agent 而 Agent**。代码探索和审查优先尝试单模型配合丰富工具（`read_file`、`ripgrep`、`bash`）自主探索。
> 2. 当遇到**长代码注意力稀释、复杂多维度审查漏报率高、或业务要求硬性打分质检门禁**时，再升级为 **PGE 多 Agent 架构**。

---

## 🧪 自动化测试与质量保障

项目配有完善的单元测试套件，覆盖核心执行引擎、RAG 向量检索与 RRF 融合算法、DAG 并行调度器等所有模块：

```bash
# 运行全项目单元测试
go test -v ./...

# 运行竞态并发安全检测
go test -v -race ./...
```

---

## 📄 License

MIT License.
