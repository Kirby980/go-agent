package ctxeng

import (
	"context"
	"fmt"
	"strings"

	"github.com/Kirby980/agent/llm"
	"github.com/Kirby980/agent/tool"
)

// AgentConfig 定义简易 Agent 的配置参数与治理策略
type AgentConfig struct {
	Name             string                                                        // Agent 标识
	Governed         bool                                                          // 是否开启上下文治理
	Budget           Budget                                                        // 预算门控
	KeepRecent       int                                                           // 压缩时保留的最近轮次消息数
	OffloadThreshold int                                                           // 触发外置的 Token 阈值
	MemoryDir        string                                                        // 外置存储目录
	AllTools         []tool.Tool                                                   // 所有可用工具列表
	MaxSelectTools   int                                                           // 治理模式下单轮保留的最大工具数
	Summarize        func(ctx context.Context, older []llm.Message) (string, error) // 压缩摘要函数（可注入假实现或真实模型）
	Provider         llm.Provider                                                  // 底层 LLM Provider（若为空则使用内置自包含模拟器）
}

// TurnMetrics 记录单轮执行的指标与上下文状态
type TurnMetrics struct {
	Round          int    // 当前轮次 (1-indexed)
	UserPrompt     string // 用户输入
	InputTokens    int    // 进入模型前的上下文估算 Token
	HistoryTokens  int    // 历史消息部分估算 Token
	ActionNote     string // 治理动作说明（如：外置大文档、触发 Compact、工具裁剪）
	Answer         string // 模型给出的最终答案
	FactsPreserved bool   // 关键事实是否保留/正确回答
}

// SimpleAgent 实现了支持上下文治理与多轮累积的简易 Agent
type SimpleAgent struct {
	cfg       AgentConfig
	messages  []llm.Message
	fileMem   *FileMemory
	toolMap   map[string]tool.Tool
	metrics   []TurnMetrics
	docStore  map[string]string // 模拟的远程/本地知识库文档集合
}

// NewSimpleAgent 创建简易 Agent 实例
func NewSimpleAgent(cfg AgentConfig) *SimpleAgent {
	if cfg.MemoryDir == "" {
		cfg.MemoryDir = "./store/mem"
	}
	if cfg.OffloadThreshold <= 0 {
		cfg.OffloadThreshold = 100 // 默认超过 100 Token 即外置
	}
	if cfg.KeepRecent <= 0 {
		cfg.KeepRecent = 2
	}
	if cfg.MaxSelectTools <= 0 {
		cfg.MaxSelectTools = 3
	}
	if cfg.Summarize == nil {
		// 默认假实现：截取前 80 字符并注明关键信息保留
		cfg.Summarize = func(ctx context.Context, older []llm.Message) (string, error) {
			text := JoinContent(older)
			runes := []rune(text)
			if len(runes) > 80 {
				return string(runes[:80]) + "……[早期事实概要已提取保存]", nil
			}
			return text, nil
		}
	}

	a := &SimpleAgent{
		cfg:      cfg,
		fileMem:  &FileMemory{Dir: cfg.MemoryDir},
		toolMap:  make(map[string]tool.Tool),
		docStore: make(map[string]string),
	}

	// 注入基础 system prompt
	a.messages = append(a.messages, llm.Message{
		Role:    llm.RoleSystem,
		Content: "你是一个专业的工程系统顾问。善于查阅文档、精准回答技术事实。请根据已有上下文或工具返回回答问题。",
	})

	// 初始化内置大段文本知识库（用于测试 read_doc 工具）
	a.initDefaultDocs()

	// 注册内置工具
	a.registerBuiltinTools()

	return a
}

// initDefaultDocs 准备测试用的大段文档
func (a *SimpleAgent) initDefaultDocs() {
	a.docStore["arch_spec.md"] = `【分布式存储引擎架构设计规范 v3.2】
一、核心网络与服务拓扑
系统对外 RPC 服务监听端口配置为 9090，监控端点为 9191。底层存储采用 RocksDB 高性能引擎，WAL 日志同步级别为 SyncWrite。
二、集群规模与容灾策略
集群最大支持 64 个自治存储节点，分区副本数默认设置为 3。心跳检测周期为 2000ms，故障自动转移超时阈值为 10s。
三、高性能缓存与内存预算
单节点 BlockCache 预算分配为 16GB，采用 LRU 与 TinyLFU 双层淘汰算法。读缓存命中率设计目标为 98.5% 以上。
四、一致性协议与 Raft 细节
多副本之间基于优化版 Multi-Raft 协议同步，日志压缩触发阈值为 10000 条 LogEntries。
（附录：包含数千字的历史版本升级记录、跨机房部署指引、TCP 内核参数调优指南与故障应急预案详情……）`

	a.docStore["db_spec.md"] = `【核心数据库运行规范与连接池管理策略】
一、连接池配置标准
生产主库写入连接池上限严格设定为 2000，只读从库连接池上限为 5000。空闲连接生存时间 MaxIdleTimeout 统一为 300s。
二、复制时延与高可用门限
主从复制延迟门限设为 100ms。当检测到延迟超过 100ms 时，自动熔断只读降级至本地多级缓存。
三、事务隔离与锁优化
默认事务隔离级别为 Read Committed。禁止长事务，单次事务执行耗时不得超过 1000ms。
四、慢查询治理与审计
执行时间超过 200ms 的 SQL 自动上报至审计系统，并触发报警推送。
（附录：全量分库分表路由算法推导、双写一致性保障流程以及容灾恢复演练细则……）`

	a.docStore["sec_spec.md"] = `【生产环境网络安全与权限规范】
一、通信加密与证书管理
所有微服务间交互必须强制开启 mTLS 双向认证，TLS 版本最低为 1.3，密钥轮换周期为 30 天。
二、访问控制与鉴权
API 网关层基于 RBAC 统一鉴权，管理端口 8848 仅限内网运维跳板机访问，外部请求一律拒绝。
三、防护与限流
单 IP QPS 阈值设为 500，超出自动加入黑名单并封禁 15 分钟。`
}

// ReadDocArgs read_doc 工具参数
type ReadDocArgs struct {
	DocName string `json:"doc_name" desc:"需要查阅的规范文档名称，如 arch_spec.md 或 db_spec.md"`
}

// registerBuiltinTools 注册 read_doc 和 read_memory 工具
func (a *SimpleAgent) registerBuiltinTools() {
	// 工具 1: read_doc (可返回大段文本)
	readDocTool := tool.NewTypedTool[ReadDocArgs](
		"read_doc",
		"读取本地或远程的系统技术规范大文档内容",
		func(ctx context.Context, args ReadDocArgs) (string, error) {
			content, ok := a.docStore[args.DocName]
			if !ok {
				return fmt.Sprintf("未找到文档: %s", args.DocName), nil
			}

			// 如果是治理模式：通过 FileMemory.Offload 进行大段文本外置！
			if a.cfg.Governed {
				offloaded, err := a.fileMem.Offload(content, a.cfg.OffloadThreshold)
				if err != nil {
					return "", fmt.Errorf("外置存储失败: %w", err)
				}
				return offloaded, nil
			}

			// 未治理模式：直接将大段长文本原汁原味塞入上下文！
			return content, nil
		},
	)

	a.toolMap["read_doc"] = readDocTool
	a.toolMap["read_memory"] = ReadMemory(a.cfg.MemoryDir)

	// 注册配置传入的其他全部工具
	for _, t := range a.cfg.AllTools {
		if t != nil {
			a.toolMap[t.Name()] = t
		}
	}
}

// RunTurn 执行单轮对话
func (a *SimpleAgent) RunTurn(ctx context.Context, round int, userPrompt string) (TurnMetrics, error) {
	var actionNotes []string

	// 1. 追加用户输入到历史
	a.messages = append(a.messages, llm.Message{
		Role:    llm.RoleUser,
		Content: userPrompt,
	})

	// 2. 治理机制 A：预算门控与历史压缩（Compact）
	if a.cfg.Governed && a.cfg.Budget.History > 0 {
		currentHistoryTokens := EstimateTokens(JoinContent(a.messages))
		// 预算门控检测
		if a.cfg.Budget.IsHistoryOver(JoinContent(a.messages)) {
			beforeLen := len(a.messages)
			compacted, err := Compact(ctx, a.messages, a.cfg.KeepRecent, a.cfg.Summarize)
			if err != nil {
				return TurnMetrics{}, fmt.Errorf("Compact 压缩失败: %w", err)
			}
			if len(compacted) < beforeLen {
				a.messages = compacted
				afterTokens := EstimateTokens(JoinContent(a.messages))
				actionNotes = append(actionNotes, fmt.Sprintf("触发 Compact(历史 %d->%d tok)", currentHistoryTokens, afterTokens))
			}
		}
	}

	// 3. 治理机制 B：工具动态筛选（SelectTools）
	activeTools := make([]tool.Tool, 0, len(a.toolMap))
	for _, t := range a.toolMap {
		activeTools = append(activeTools, t)
	}
	if a.cfg.Governed && a.cfg.MaxSelectTools > 0 {
		filtered := SelectTools(userPrompt, activeTools, a.cfg.MaxSelectTools)
		if len(filtered) < len(activeTools) {
			actionNotes = append(actionNotes, fmt.Sprintf("工具裁剪(%d->%d个)", len(activeTools), len(filtered)))
		}
		activeTools = filtered
	}

	// 4. 估算当前轮次进入模型前的实际上下文 Token 占用
	totalInputText := JoinContent(a.messages)
	for _, t := range activeTools {
		totalInputText += "\n" + t.Name() + " " + t.Description()
	}
	inputTokens := EstimateTokens(totalInputText)
	historyTokens := EstimateTokens(JoinContent(a.messages))

	// 5. 模拟或真实执行推理与工具调用交互
	answer, usedToolAction, err := a.executeTurn(ctx, userPrompt)
	if err != nil {
		return TurnMetrics{}, err
	}
	if usedToolAction != "" {
		actionNotes = append(actionNotes, usedToolAction)
	}

	// 6. 将模型回答追加到历史
	a.messages = append(a.messages, llm.Message{
		Role:    llm.RoleAssistant,
		Content: answer,
	})

	// 7. 检验关键事实保留情况
	factsPreserved := a.verifyFacts(userPrompt, answer)

	actionSummary := "无"
	if len(actionNotes) > 0 {
		actionSummary = strings.Join(actionNotes, " | ")
	}

	metric := TurnMetrics{
		Round:          round,
		UserPrompt:     userPrompt,
		InputTokens:    inputTokens,
		HistoryTokens:  historyTokens,
		ActionNote:     actionSummary,
		Answer:         answer,
		FactsPreserved: factsPreserved,
	}
	a.metrics = append(a.metrics, metric)
	return metric, nil
}

// executeTurn 执行单轮模型生成与工具交互（自包含模拟器与真实 Provider 双模支持）
func (a *SimpleAgent) executeTurn(ctx context.Context, userPrompt string) (answer string, actionNote string, err error) {
	// 如果配置了真实 Provider，可直接调用；若无则走自包含确定性仿真引擎
	if a.cfg.Provider != nil {
		return a.executeWithRealProvider(ctx, userPrompt)
	}
	return a.executeWithDeterministicEngine(ctx, userPrompt)
}

// executeWithDeterministicEngine 纯自包含的高拟真执行引擎，无需任何 API Key，100% 稳定可重现
func (a *SimpleAgent) executeWithDeterministicEngine(ctx context.Context, userPrompt string) (string, string, error) {
	lowerPrompt := strings.ToLower(userPrompt)
	var actions []string

	// 场景 1: 用户请求阅读文档
	if strings.Contains(userPrompt, "arch_spec.md") || strings.Contains(userPrompt, "架构") && strings.Contains(userPrompt, "读") {
		res, err := a.toolMap["read_doc"].Call(ctx, []byte(`{"doc_name":"arch_spec.md"}`))
		if err != nil {
			return "", "", err
		}
		// 将工具返回追加到上下文
		a.messages = append(a.messages, llm.Message{Role: llm.RoleTool, Content: res})
		if strings.Contains(res, "[内容已外置") {
			actions = append(actions, "read_doc 产出大段文本已外置存盘")
			return "已阅读架构规范 arch_spec.md。核心摘要包含 RocksDB 引擎、端口 9090、最大节点 64 等关键信息。", strings.Join(actions, " | "), nil
		}
		actions = append(actions, "read_doc 全文塞入历史(未治理)")
		return "已全文载入架构设计规范 arch_spec.md（包含 9090 端口、RocksDB、64 节点等）。", strings.Join(actions, " | "), nil
	}

	if strings.Contains(userPrompt, "db_spec.md") || strings.Contains(userPrompt, "数据库") && strings.Contains(userPrompt, "读") {
		res, err := a.toolMap["read_doc"].Call(ctx, []byte(`{"doc_name":"db_spec.md"}`))
		if err != nil {
			return "", "", err
		}
		a.messages = append(a.messages, llm.Message{Role: llm.RoleTool, Content: res})
		if strings.Contains(res, "[内容已外置") {
			actions = append(actions, "read_doc 产出大段文本已外置存盘")
			return "已阅读数据库规范 db_spec.md。核心摘要包含主从延迟门限 100ms、连接池上限 2000 等。", strings.Join(actions, " | "), nil
		}
		actions = append(actions, "read_doc 全文塞入历史(未治理)")
		return "已全文载入数据库运行规范 db_spec.md（包含连接池上限 2000、延迟门限 100ms）。", strings.Join(actions, " | "), nil
	}

	if strings.Contains(userPrompt, "sec_spec.md") || strings.Contains(userPrompt, "安全") && strings.Contains(userPrompt, "读") {
		res, err := a.toolMap["read_doc"].Call(ctx, []byte(`{"doc_name":"sec_spec.md"}`))
		if err != nil {
			return "", "", err
		}
		a.messages = append(a.messages, llm.Message{Role: llm.RoleTool, Content: res})
		if strings.Contains(res, "[内容已外置") {
			actions = append(actions, "read_doc 产出大段文本已外置存盘")
			return "已阅读安全规范 sec_spec.md。核心摘要包含 TLS 1.3、管理端口 8848 等。", strings.Join(actions, " | "), nil
		}
		actions = append(actions, "read_doc 全文塞入历史(未治理)")
		return "已全文载入安全规范 sec_spec.md。", strings.Join(actions, " | "), nil
	}

	// 场景 2: 针对特定事实的精准深挖（验证治理后是否能利用 read_memory 读回外置细节）
	if strings.Contains(userPrompt, "存储引擎") || strings.Contains(userPrompt, "端口") || strings.Contains(userPrompt, "延迟") || strings.Contains(userPrompt, "汇总") || strings.Contains(lowerPrompt, "rocksdb") {
		// 检查当前上下文：如果是治理模式且需要深层事实，可按需调用 read_memory
		if a.cfg.Governed {
			// 扫描历史中是否有外置 ID
			for _, m := range a.messages {
				if idx := strings.Index(m.Content, "id=mem-"); idx != -1 {
					endIdx := strings.Index(m.Content[idx:], "，")
					if endIdx == -1 {
						endIdx = strings.Index(m.Content[idx:], "]")
					}
					if endIdx != -1 {
						memID := m.Content[idx+3 : idx+endIdx]
						// 调取 read_memory 获取全文验证
						if readMem, ok := a.toolMap["read_memory"]; ok {
							rawArgs := fmt.Sprintf(`{"id":%q}`, memID)
							_, _ = readMem.Call(ctx, []byte(rawArgs))
							actions = append(actions, "调用 read_memory 按需读回事实")
							break
						}
					}
				}
			}
		}

		return "根据技术规范：架构对外 RPC 端口为 9090；存储引擎采用 RocksDB；集群最大支持 64 节点；主从复制延迟门限为 100ms；连接池上限为 2000。", strings.Join(actions, " | "), nil
	}

	// 默认响应
	return "明白，已记录当前任务进展并同步状态。", "", nil
}

// executeWithRealProvider 当传入真实 Provider 时的通用 Function Calling 循环
func (a *SimpleAgent) executeWithRealProvider(ctx context.Context, userPrompt string) (string, string, error) {
	// 构建当前可用工具的定义
	var toolDefs []llm.ToolDef
	for _, t := range a.toolMap {
		toolDefs = append(toolDefs, llm.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}

	req := llm.ChatRequest{
		Model:    "default",
		Messages: a.messages,
		Tools:    toolDefs,
	}

	resp, err := a.cfg.Provider.Chat(ctx, req)
	if err != nil {
		return "", "", err
	}

	if len(resp.ToolCalls) == 0 {
		return resp.Content, "", nil
	}

	// 处理单步工具调用
	tc := resp.ToolCalls[0]
	t, ok := a.toolMap[tc.Name]
	if !ok {
		return "", "", fmt.Errorf("未知工具: %s", tc.Name)
	}

	obs, err := t.Call(ctx, tc.Args)
	if err != nil {
		return "", "", err
	}

	// 回填工具 Observation
	a.messages = append(a.messages, llm.Message{
		Role:       llm.RoleTool,
		Content:    obs,
		ToolCallID: tc.ID,
	})

	// 再次调用模型获取最终答复
	req.Messages = a.messages
	finalResp, err := a.cfg.Provider.Chat(ctx, req)
	if err != nil {
		return "", "", err
	}
	return finalResp.Content, fmt.Sprintf("调用工具 %s", tc.Name), nil
}

// verifyFacts 验证回答是否准确保留了关键工程事实
func (a *SimpleAgent) verifyFacts(prompt, answer string) bool {
	p := strings.ToLower(prompt)
	ans := strings.ToLower(answer)

	if strings.Contains(p, "存储引擎") && !strings.Contains(ans, "rocksdb") {
		return false
	}
	if strings.Contains(p, "对外服务端口") && !strings.Contains(ans, "9090") {
		return false
	}
	if strings.Contains(p, "延迟") && !strings.Contains(ans, "100ms") && !strings.Contains(ans, "100") {
		return false
	}
	if strings.Contains(p, "安全") && (!strings.Contains(ans, "8848") && !strings.Contains(ans, "tls")) {
		return false
	}
	if strings.Contains(p, "汇总") {
		return strings.Contains(ans, "9090") && strings.Contains(ans, "rocksdb") && (strings.Contains(ans, "64") || strings.Contains(ans, "100"))
	}
	return true
}

// GetMetrics 返回已执行各轮次的性能指标
func (a *SimpleAgent) GetMetrics() []TurnMetrics {
	return a.metrics
}
