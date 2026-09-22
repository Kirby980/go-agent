# Go AI Agent Framework

一个用 Go 语言编写的高性能、生产级 AI Agent 核心框架。具备类似 **Claude Code / AGY** 的终端交互体验，支持原生的 **Function Calling** 与经典 **ReAct（Reasoning + Acting）** 自愈模式、**多智能体分治协同（Subagent 委派 + Supervisor 团队流水线）**、企业级 **双路混合 RAG（基于自研 go-es 驱动）**、**本地代码库自主阅读与终端工具（内置 Human-in-the-Loop 安全拦截）**、多模型统一抽象与路由容灾、DAG 任务规划并行调度，以及 Model Context Protocol (MCP) 与 Agent Skills 扩展协议。

---

## 🌟 核心特性

### 1. 终端交互与行编辑器 (对标 Claude Code / AGY)
- **实时行内建议菜单**：在空行键入 `/` 的瞬间，光标下方即时展开可用斜杠指令列表，无需等待回车。
- **固定视口与平滑滚动**：下拉框大小恒定为 6 行，支持 `↑` / `↓` 移动绿色高亮光标 `❯`；到达底部或顶部时稳固停驻（不无限循环），超出视口时内容平滑滚动。
- **字符实时过滤与回退**：输入 `/m` 建议框自动过滤只保留 `/model`；按 Backspace 删掉 `/` 建议框立即隐去，恢复普通文本输入。
- **本地 Shell 命令直通 (`!<cmd>`)**：输入 `!git status`、`!go test ./...`、`!ls -la` 直接在本地宿主机 Shell 执行，保留原生彩色输出与终端交互。
- **文件绝对路径智能消歧**：输入 `/home/...` 或任何带多级目录 `/` 的路径时，编辑器自动判定为提问内容而非指令，不会误拦截，Agent 可正常阅读文件。
- **全套快捷键支持**：支持历史记录漫游（`↑` / `↓`）、`Ctrl+A` / `Ctrl+E` / `Ctrl+K` / `Ctrl+U`、`Ctrl+C` 取消当前行、`Ctrl+D` 退出。

### 2. 多智能体体系 (Multi-Agent Systems)
- **层次化 Subagent 委派模式 (日常默认)**：
  - 主 Agent 作为面向用户的交互中枢，挂载 `delegate_subagent` 工具；
  - 遇到耗时、长链路或复杂任务时，自主召唤拥有**物理隔离全新上下文**的专职子智能体（如 `researcher` 探索代码、`coder` 精准重构、`reviewer` 审查测试）；
  - 子智能体的大量中间工具调用和试错日志完全隔离，执行完毕后只向主 Agent 回传精炼结论，**彻底解决主上下文爆炸与注意力稀释**；
  - 终端以带颜色标签实时流式呈现 Subagent 的工作细节（`[researcher 思考]`、`[researcher 工具]`）。
- **Supervisor-Worker 团队多智能体 (`/team`)**：
  - 输入 `/team <任务>` 调起自动化多智能体协同流水线；
  - 由中央大脑 **Supervisor（主管）** 动态审视阶段性进展，自主在 `researcher`、`coder`、`reviewer` 之间动态派活推进，直到全案达成 `FINISH` 交付。
- **多样化多智能体编排范式 (`mas/` 包)**：
  - **Supervisor**：中心化主管督导与任务指派；
  - **Isolated Orchestrator**：强隔离分治编排 + Sectioning 并发调度；
  - **Swarm**：基于接力转交（Handoff）的去中心化多智能体协作（OpenAI Swarm 理念）；
  - **Pipeline**：顺序流水线链式推进；
  - **Multi-Agent Debate**：多智能体交叉辩论与多数表决达成共识；
  - **GroupChat**：基于消息总线（MessageBus）广播的圆桌讨论组。

### 3. 推理引擎与上下文缓存 (Prompt Caching)
- **原生 Function Calling 与 ReAct 双模式**：支持大模型结构化 Tool Calling 能力；面向纯文本模型自动降级为 ReAct（Thought-Action-Observation），内置格式解析失败自愈重试。
- **Anthropic Claude Prompt Caching 深度集成**：
  - 精确计算 billable prompt tokens（`input_tokens + cache_read + cache_creation`）；
  - 在非流式与流式输出中均实时统计 `CachedTokens` 与命中率，通过 `/stats` 查看会话节省情况。

### 4. 企业级双路混合检索 RAG (默认基于 `Kirby980/go-es`)
- **通用 VectorStore 抽象**：面向接口解耦，工厂配置支持 Elasticsearch 8.x/9.x 与 PostgreSQL (pgvector)。
- **自研 ES 客户端深度集成**：接入 [`github.com/Kirby980/go-es`](https://github.com/Kirby980/go-es)，利用 `BulkBuilder` 零反射批量入库，`SearchBuilder.KNN` 运行原生向量检索。
- **双路召回 + RRF 融合**：KNN 语义向量检索 + BM25 倒排关键词检索，使用 RRF（倒数排名融合）算法智能排序，并支持二次精排（Reranker）。

### 5. 本地代码感知与 Human-in-the-Loop 安全卫士
- **代码库阅读与定位**：内置 `read_file`、`list_dir`、`write_file`、`replace_content`，大模型自主探索并重构代码。
- **终端执行与安全审批 (Approver)**：安全命令自动放行；破坏性命令（`rm`, `mv`, `sudo`, 重定向 `>`）触发终端交互式 `[y/a/n]` 人类确认。

### 6. DAG 任务规划与拓扑并行执行 (`plan/`)
- 基于 Kahn 算法进行依赖图拓扑分层（`Levels`），严格检测循环依赖。
- 同层任务并发调度，层间依赖同步等待；支持单点故障快速熔断取消（`context.WithCancel`）。

---

## 🏗️ 架构分层

```text
┌────────────────────────────────────────────────────────┐
│                        cmd/                            │
│    CLI 入口 (main.go)、实时行编辑器 (editor.go)        │
│    Subagent 工具装配、/team 团队模式调度、!shell 直通    │
└──────────────────────────┬─────────────────────────────┘
                           ▼
┌────────────────────────────────────────────────────────┐
│                       agent/                           │
│   Agent 核心调度引擎 (RunStream, ReAct, FunctionCall)    │
│   人类在环审批 (Approver)、预算控制 (Budget)、存储 (Store)│
└──────────┬───────────────────┬─────────────────────────┘
           │                   │
           ▼                   ▼
┌──────────────────────┐ ┌──────────────────────────────────────┐
│        mas/          │ │                tool/                 │
│  6 大多智能体架构体系  │ │         工具契约、类型安全与注册表       │
│  (Supervisor, Swarm, │ └──────────┬──────────────┬────────────┘
│   Orchestrator, ...) │            │              │
└──────────────────────┘            │              │
           │                        ▼              ▼
           │             ┌────────────────────┐ ┌────────────────────┐
           ▼             │      builtin/      │ │        rag/        │
┌──────────────────────┐ │ 文件操作、终端执行 │ │ 企业级 RAG 混合检索│
│        plan/         │ │ 沙箱隔离环境       │ │ KNN+BM25+RRF 融合  │
│  DAG 拓扑分层与并行   │ └────────────────────┘ └────────────────────┘
└──────────────────────┘                                   │
                                                           ▼
┌───────────────────────────────────────────────────────────────────┐
│                               router/                             │
│                      多模型优先级路由与自动降级容灾                 │
└──────────────────────────────────┬────────────────────────────────┘
                                   ▼
┌───────────────────────────────────────────────────────────────────┐
│                                llm/                               │
│        统一 Provider 接口、Claude (Prompt Caching)、OpenAI 兼容    │
└───────────────────────────────────────────────────────────────────┘
```

---

## 📁 目录结构

```text
.
├── cmd/                         # [CLI 交互入口]
│   ├── main.go                  # 主程序循环、/team 团队模式、delegate_subagent 工具装配
│   ├── editor.go                # 原生交互式终端行编辑器（实时下拉菜单、固定视口滚动、!shell）
│   ├── editor_test.go           # 行编辑器单元测试（截断、无循环边界、消歧测试）
│   └── config.go                # CLI 配置加载与环境变量映射
├── agent/                       # [核心引擎] Agent 单体执行引擎与调度中心
│   ├── agent.go                 # Agent 结构体定义、配置项与主调度循环 (Run / RunStream)
│   ├── approver.go              # 人类在环 (HITL) 命令审计与交互式放行
│   ├── budget.go                # 运行预算控制 (Token 上限、步数、死循环检测)
│   ├── event.go                 # 流式事件类型 (Thought, ToolCall, Result 等)
│   ├── function_call.go         # 原生 Function Calling 执行驱动
│   ├── react.go                 # ReAct 自愈提示词与解析执行循环
│   ├── state.go                 # 运行状态快照与上下文历史
│   └── store.go                 # 会话持久化接口与 FileStore 实现
├── mas/                         # [多智能体系统 MAS] 6 种经典多智能体协作范式
│   ├── supervisor.go            # 主管-工作者 (Supervisor-Worker) 中心督导模式
│   ├── orchestrator.go          # 分治编排与上下文物理隔离 (Isolated Orchestrator)
│   ├── swarm.go                 # 去中心化接力转交模式 (OpenAI Swarm 理念)
│   ├── pipeline.go              # 链式管道流水线 (Pipeline)
│   ├── multi-agent-debate.go    # 交叉辩论与一致性共识 (Multi-Agent Debate)
│   ├── chat.go                  # 单轮 LLM 对话辅助函数
│   ├── msg.go                   # 统一消息模型
│   └── msg-bus.go               # 事件总线广播圆桌会议 (GroupChat)
├── plan/                        # [DAG 规划] 任务依赖图规划执行器
│   ├── plan.go                  # Task 与 Plan 结构定义
│   ├── levels.go                # 基于 Kahn 算法的拓扑分层与循环依赖检测
│   └── execute.go               # 按拓扑层级并行并发执行
├── builtin/                     # [内置工具]
│   ├── bash.go                  # 终端命令执行工具 (超时控制、输出截断防护)
│   ├── file.go                  # 代码阅读 (read_file)、写文件与替换内容
│   ├── nl2sql.go                # 自然语言转 SQL 工具
│   └── sandbox.go               # 沙箱隔离环境
├── rag/                         # [企业级 RAG]
│   ├── store.go                 # 通用 VectorStore 接口定义与工厂
│   ├── es.go                    # 基于 Kirby980/go-es 的高性能 ES 驱动 (KNN + BM25)
│   ├── pgx.go                   # 基于 pgx + pgvector 的 PostgreSQL 驱动
│   ├── rerank.go                # Retriever 检索编排器 (双路召回 + 重排)
│   ├── rrf.go                   # RRF (倒数排名融合) 算法
│   └── chuck.go                 # 递归中文友好文本分块器
├── tool/                        # [工具契约] Tool 接口、强类型泛型工具 (TypedTool) 与注册表
├── skill/                       # [技能生态] SKILL.md 规范自动扫描与挂载
├── mcp/                         # [协议生态] Model Context Protocol (MCP) 客户端与工具桥接
├── llm/                         # [模型层] 统一 Provider、Claude (Prompt Caching)、OpenAI 兼容
└── router/                      # [模型路由器] 多 Provider 轮询与主备降级
```

---

## 🚀 快速上手

### 1. 启动交互式 CLI 终端助手

配置环境变量或配置文件后直接启动：

```bash
export supplier="anthropic"       # 或 "openai"
export name="claude-3-5-sonnet"   # 或 "gemini-2.5-pro", "gpt-4o"
export base_url="https://api.anthropic.com/v1"
export key="your-api-key"

go run ./cmd
```

启动后会看到交互式欢迎界面：
```text
╭─────────────────────────────────────────────────────────────╮
│   ✦ Go-Agent CLI (Codex / Claude Code 架构)                 │
│   会话: default      | 模型: claude-3-5-sonnet               │
│   工作目录: /home/user/myproject                            │
│   输入 / 唤起指令菜单, !<命令> 执行 Shell, /exit 退出        │
╰─────────────────────────────────────────────────────────────╯

agent (default) > 
```

---

### 2. 交互式指令与本地 Shell 直通

在输入行中：
- **键入 `/`**：立即弹出指令候选列表，通过 `↑` / `↓` 移动选择，`Tab` 补全，`Enter` 确认：
  ```text
  agent (default) > /
    ❯ /help      - 打印帮助信息与支持的指令列表
      /new       - 开启全新对话会话 (重置当前记忆)
      /resume    - 恢复指定名称的历史会话记忆
      /sessions  - 列出本地保存的所有历史会话记录
      /model     - 查看当前活动模型或切换新模型
      /team      - 启动 Supervisor 多智能体团队协同执行复杂任务
      /stats     - 查看当前会话 Token 用量与缓存命中率
      /clear     - 清空当前控制台屏幕
      /exit      - 退出 CLI 程序
  ```
- **输入 `!<cmd>` 直通本地 Shell**：
  ```bash
  agent (default) > !git status
  agent (default) > !go test ./...
  agent (default) > !pwd
  ```

---

### 3. 多智能体协作实战

#### ① 自动 Subagent 委派（日常对话）
在日常对话中遇到繁琐复杂任务时，主 Agent 会自主调用 `delegate_subagent` 工具，在物理隔离的全新上下文中召唤专职子 Agent：
```text
agent (default) > 调研 plan 模块和 mas 模块的架构设计，对比两者的调度差异并总结汇报

[调用工具] delegate_subagent({"role":"researcher","task":"调研 plan 与 mas 模块..."})

┌── 🤖 [启动 Subagent: researcher] ───────────────────────────────
│ 目标: 调研 plan 与 mas 模块...
└────────────────────────────────────────────────────────
  [researcher 工具] list_dir({"DirectoryPath":"plan"})
  [researcher 结果] [...]
  [researcher 思考] 分析拓扑调度模式...
✔ [Subagent: researcher 执行完成]

[工具结果] 【子智能体 researcher 的执行汇报】...
```

#### ② `/team` 主管督导团队模式
针对复杂工程改造，直接通过 `/team` 命令唤起多智能体团队：
```bash
agent (default) > /team 审计 mas 模块的代码质量并编写单元测试
```
系统将启动 Supervisor 调度 `researcher`、`coder`、`reviewer` 三位专职成员链式推进，并在控制台实时呈现每轮指派与交付成果，直到全案达成 `FINISH`。

---

### 4. 代码中直接调用多智能体与 DAG 编排

#### ① 调用 Supervisor 团队
```go
package main

import (
	"context"
	"fmt"

	"github.com/Kirby980/agent/agent"
	"github.com/Kirby980/agent/mas"
)

func main() {
	ctx := context.Background()

	workers := map[string]*agent.Agent{
		"researcher": agent.New(provider, "claude-3-5-sonnet", registry),
		"coder":      agent.New(provider, "claude-3-5-sonnet", registry),
		"reviewer":   agent.New(provider, "claude-3-5-sonnet", registry),
	}

	sup := &mas.Supervisor{
		Provider: provider,
		Model:    "claude-3-5-sonnet",
		Workers:  workers,
		MaxTurns: 8,
	}

	result, err := sup.Run(ctx, "重构用户认证模块并补充单测")
	if err != nil {
		panic(err)
	}
	fmt.Println(result)
}
```

#### ② 调用 DAG 拓扑分层任务编排
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
			{ID: "fetch_api", Tool: "read_file", Args: json.RawMessage(`{"path":"api.md"}`)},
			{ID: "fetch_db", Tool: "read_file", Args: json.RawMessage(`{"path":"schema.sql"}`)},
			// analyze 依赖前两项任务的结果，前两项并行执行完成后自动触发 analyze
			{ID: "analyze", Tool: "diff", DependsOn: []string{"fetch_api", "fetch_db"}},
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

## 🧪 自动化测试与质量保障

项目配有完整的单元测试套件：

```bash
# 运行全部单元测试
go test -v ./...

# 运行竞态并发安全检测
go test -v -race ./...
```

---

## 📄 License

MIT License.
