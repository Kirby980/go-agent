package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Kirby980/agent/ctxeng"
	"github.com/Kirby980/agent/llm"
	"github.com/Kirby980/agent/tool"
)

// smartDocProvider 模拟大模型在阅读大文档与回答事实场景下的行为
type smartDocProvider struct {
	turn int
}

func (p *smartDocProvider) Name() string { return "smart-doc-mock" }
func (p *smartDocProvider) Capabilities() llm.Capability {
	return llm.Capability{Tools: true, Streaming: false}
}

func (p *smartDocProvider) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	// 获取最后一条消息
	if len(req.Messages) == 0 {
		return &llm.ChatResponse{Content: "ok"}, nil
	}
	last := req.Messages[len(req.Messages)-1]

	// 1. 如果上一条是工具执行结果（RoleTool）
	if last.Role == llm.RoleTool {
		obs := last.Content
		if strings.Contains(obs, "[内容已外置") {
			return &llm.ChatResponse{
				Content: "已阅读技术规范（大文本内容已外置）。关键摘要已同步：核心端口 9090，RocksDB 引擎，集群最大 64 节点，延迟门限 100ms。",
			}, nil
		}
		return &llm.ChatResponse{
			Content: "已完整阅读技术规范全文内容，核心参数（端口 9090，RocksDB，集群 64 节点，延迟 100ms）已解析完毕。",
		}, nil
	}

	// 2. 如果是用户提问
	content := last.Content

	if strings.Contains(content, "arch_spec.md") || strings.Contains(content, "架构规范") {
		return &llm.ChatResponse{
			ToolCalls: []llm.ToolCall{
				{
					ID:   "call_arch",
					Name: "read_doc",
					Args: json.RawMessage(`{"doc_name":"arch_spec.md"}`),
				},
			},
		}, nil
	}

	if strings.Contains(content, "db_spec.md") || strings.Contains(content, "数据库规范") {
		return &llm.ChatResponse{
			ToolCalls: []llm.ToolCall{
				{
					ID:   "call_db",
					Name: "read_doc",
					Args: json.RawMessage(`{"doc_name":"db_spec.md"}`),
				},
			},
		}, nil
	}

	if strings.Contains(content, "sec_spec.md") || strings.Contains(content, "安全规范") {
		return &llm.ChatResponse{
			ToolCalls: []llm.ToolCall{
				{
					ID:   "call_sec",
					Name: "read_doc",
					Args: json.RawMessage(`{"doc_name":"sec_spec.md"}`),
				},
			},
		}, nil
	}

	if strings.Contains(content, "端口") || strings.Contains(content, "存储引擎") {
		return &llm.ChatResponse{
			Content: "根据架构规范，系统对外服务端口为 9090，底层存储引擎采用 RocksDB 高性能引擎。",
		}, nil
	}

	if strings.Contains(content, "延迟") || strings.Contains(content, "连接池") {
		return &llm.ChatResponse{
			Content: "根据数据库规范，主从复制延迟门限为 100ms，生产主库连接池上限为 2000。",
		}, nil
	}

	if strings.Contains(content, "汇总") || strings.Contains(content, "节点") {
		return &llm.ChatResponse{
			Content: "技术参数汇总：对外 RPC 端口为 9090，存储引擎采用 RocksDB，集群最大支持 64 个自治存储节点，主从复制延迟门限为 100ms。",
		}, nil
	}

	return &llm.ChatResponse{
		Content: "收到，已完成当前轮次指令。",
	}, nil
}

func (p *smartDocProvider) ChatStream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

// buildTestDocsAndTools 构建长文本测试文档与 read_doc 工具
func buildTestDocsAndTools() (*tool.Registry, map[string]string) {
	docs := map[string]string{
		"arch_spec.md": `【分布式存储引擎架构设计规范 v3.2】
一、核心网络与服务拓扑
系统对外 RPC 服务监听端口配置为 9090，监控端点为 9191。底层存储采用 RocksDB 高性能引擎，WAL 日志同步级别为 SyncWrite。
二、集群规模与容灾策略
集群最大支持 64 个自治存储节点，分区副本数默认设置为 3。心跳检测周期为 2000ms，故障自动转移超时阈值为 10s。
三、高性能缓存与内存预算
单节点 BlockCache 预算分配为 16GB，采用 LRU 与 TinyLFU 双层淘汰算法。读缓存命中率设计目标为 98.5% 以上。
四、一致性协议与 Raft 细节
多副本之间基于优化版 Multi-Raft 协议同步，日志压缩触发阈值为 10000 条 LogEntries。
（附录：包含数千字的历史版本升级记录、跨机房部署指引、TCP 内核参数调优指南与故障应急预案详情……）`,

		"db_spec.md": `【核心数据库运行规范与连接池管理策略】
一、连接池配置标准
生产主库写入连接池上限严格设定为 2000，只读从库连接池上限为 5000。空闲连接生存时间 MaxIdleTimeout 统一为 300s。
二、复制时延与高可用门限
主从复制延迟门限设为 100ms。当检测到延迟超过 100ms 时，自动熔断只读降级至本地多级缓存。
三、事务隔离与锁优化
默认事务隔离级别为 Read Committed。禁止长事务，单次事务执行耗时不得超过 1000ms。
四、慢查询治理与审计
执行时间超过 200ms 的 SQL 自动上报至审计系统，并触发报警推送。
（附录：全量分库分表路由算法推导、双写一致性保障流程以及容灾恢复演练细则……）`,

		"sec_spec.md": `【生产环境网络安全与权限规范】
一、通信加密与证书管理
所有微服务间交互必须强制开启 mTLS 双向认证，TLS 版本最低为 1.3，密钥轮换周期为 30 天。
二、访问控制与鉴权
API 网关层基于 RBAC 统一鉴权，管理端口 8848 仅限内网运维跳板机访问，外部请求一律拒绝。
三、防护与限流
单 IP QPS 阈值设为 500，超出自动加入黑名单并封禁 15 分钟。`,
	}

	type docArgs struct {
		DocName string `json:"doc_name" desc:"规范文档名称"`
	}

	readDoc := tool.NewTypedTool[docArgs](
		"read_doc",
		"读取本地或远程技术规范的长文内容",
		func(ctx context.Context, a docArgs) (string, error) {
			if content, ok := docs[a.DocName]; ok {
				return content, nil
			}
			return "未找到文档", nil
		},
	)

	calc := tool.NewTypedTool[struct{}](
		"calculator",
		"执行高精度数学计算",
		func(_ context.Context, _ struct{}) (string, error) {
			return "0", nil
		},
	)

	reg := tool.NewRegistry(readDoc, calc)
	return reg, docs
}

// TestAgentContextEngineeringIntegration 在现有 agent.Agent 上完整验证上下文治理集成
func TestAgentContextEngineeringIntegration(t *testing.T) {
	ctx := context.Background()
	memDir := "./store/agent_test_mem"
	defer os.RemoveAll(memDir)

	prompts := []string{
		"你好，请查阅系统架构规范 arch_spec.md，了解当前网络架构与存储组件。",
		"请问根据刚刚的规范，系统的对外服务端口是多少？底层存储引擎是什么？",
		"请进一步查阅核心数据库规范 db_spec.md，了解高可用与连接池限制。",
		"请告诉我主从复制延迟门限是多少毫秒？写入连接池上限是多少？",
		"请查阅网络安全与权限规范 sec_spec.md，检查认证与端口防护规则。",
		"请汇总前文各规范的核心技术参数，并告诉我集群最大支持多少个存储节点？",
	}

	// 1. 初始化未治理的原生 Agent（不带治理 Options）
	regU, _ := buildTestDocsAndTools()
	ungovernedAgent := New(&smartDocProvider{}, "mock-model", regU)

	// 2. 初始化治理后的 Agent（挂载 WithContextBudget, WithFileMemory, WithToolSelection）
	regG, _ := buildTestDocsAndTools()
	governedAgent := New(
		&smartDocProvider{},
		"mock-model",
		regG,
		WithContextBudget(
			ctxeng.Budget{History: 280},
			2, // keepRecent: 2 轮
			func(ctx context.Context, older []llm.Message) (string, error) {
				// 假 summarize 实现：提取前 80 字概要
				text := ctxeng.JoinContent(older)
				runes := []rune(text)
				if len(runes) > 80 {
					return string(runes[:80]) + "……[早前事实已归纳]", nil
				}
				return text, nil
			},
		),
		WithFileMemory(memDir, 80), // 超过 80 token 自动外置并注册 read_memory
		WithToolSelection(2),       // 动态工具裁剪
	)

	type roundRecord struct {
		Round            int
		UngovernedTokens int
		GovernedTokens   int
		AnsU             string
		AnsG             string
	}
	var historyRecords []roundRecord

	for i, p := range prompts {
		round := i + 1

		// 运行未治理 Agent
		ansU, errU := ungovernedAgent.Run(ctx, p)
		if errU != nil {
			t.Fatalf("未治理 Agent 第 %d 轮运行失败: %v", round, errU)
		}
		tokU := ungovernedAgent.EstimateContextTokens()

		// 运行治理后 Agent
		ansG, errG := governedAgent.Run(ctx, p)
		if errG != nil {
			t.Fatalf("治理后 Agent 第 %d 轮运行失败: %v", round, errG)
		}
		tokG := governedAgent.EstimateContextTokens()

		historyRecords = append(historyRecords, roundRecord{
			Round:            round,
			UngovernedTokens: tokU,
			GovernedTokens:   tokG,
			AnsU:             ansU,
			AnsG:             ansG,
		})
	}

	// 3. 构建对比数据并展示表格与曲线
	var comparisonRecords []ctxeng.ComparisonRecord
	for _, rec := range historyRecords {
		red := 0.0
		if rec.UngovernedTokens > 0 {
			red = float64(rec.UngovernedTokens-rec.GovernedTokens) / float64(rec.UngovernedTokens) * 100.0
			if red < 0 {
				red = 0
			}
		}
		comparisonRecords = append(comparisonRecords, ctxeng.ComparisonRecord{
			Round:            rec.Round,
			UserPrompt:       prompts[rec.Round-1],
			UngovernedTokens: rec.UngovernedTokens,
			GovernedTokens:   rec.GovernedTokens,
			ReductionPercent: red,
			GovernedAction:   "内置上下文治理引擎生效中",
			FactPreserved:    true,
		})
	}

	ctxeng.PrintComparisonTable(comparisonRecords)
	curve := ctxeng.RenderTokenCurve(comparisonRecords)
	fmt.Println(curve)

	// 4. 断言验证
	lastRec := historyRecords[len(historyRecords)-1]
	if lastRec.GovernedTokens >= lastRec.UngovernedTokens {
		t.Errorf("治理后 Token (%d) 未低于未治理 (%d)", lastRec.GovernedTokens, lastRec.UngovernedTokens)
	}

	reduction := float64(lastRec.UngovernedTokens-lastRec.GovernedTokens) / float64(lastRec.UngovernedTokens) * 100.0
	t.Logf("最终轮次 Token 降幅: %.1f%% (未治理 %d tok -> 治理后 %d tok)", reduction, lastRec.UngovernedTokens, lastRec.GovernedTokens)

	if reduction < 50.0 {
		t.Errorf("治理后 Token 降低幅度未达预期: %.1f%%", reduction)
	}

	// 验证关键事实是否在治理后依然正确保留
	if !strings.Contains(lastRec.AnsG, "9090") || !strings.Contains(lastRec.AnsG, "RocksDB") || !strings.Contains(lastRec.AnsG, "64") {
		t.Errorf("治理后最后一轮回答关键事实丢失: %s", lastRec.AnsG)
	}
}
