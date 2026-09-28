package ctxeng

import (
	"context"
	"fmt"
	"os"
	"testing"
)

// TestCompareGovernance 运行多轮对话，完整验证治理前与治理后的 Token 曲线与事实保留情况
func TestCompareGovernance(t *testing.T) {
	ctx := context.Background()
	testMemDir := "./store/test_mem"
	defer os.RemoveAll(testMemDir)

	// 模拟 6 轮连续对话情境：
	// 包含多次查阅不同系统的上千字大文档、提问关键技术事实、以及最终全局汇总
	prompts := []string{
		"你好，请查阅系统架构规范 arch_spec.md，了解当前网络架构与存储组件。",
		"请问根据刚刚的规范，系统的对外服务端口是多少？底层存储引擎是什么？",
		"请进一步查阅核心数据库规范 db_spec.md，了解高可用与连接池限制。",
		"请告诉我主从复制延迟门限是多少毫秒？写入连接池上限是多少？",
		"请查阅网络安全与权限规范 sec_spec.md，检查认证与端口防护规则。",
		"请汇总前文各规范的核心技术参数，并告诉我集群最大支持多少个存储节点？",
	}

	// 1. 初始化【未治理 Agent】（不做外置、不设预算、不压缩、不裁剪工具）
	ungovernedAgent := NewSimpleAgent(AgentConfig{
		Name:     "Ungoverned-Agent",
		Governed: false,
	})

	// 2. 初始化【治理后 Agent】
	// - 预算门控：History 上限设为 300 Token
	// - 外置存储：大段文本超过 80 Token 自动 Offload
	// - 压缩机制：超出预算触发 Compact，保留最新 2 轮消息
	governedAgent := NewSimpleAgent(AgentConfig{
		Name:             "Governed-Agent",
		Governed:         true,
		Budget:           Budget{History: 300},
		KeepRecent:       2,
		OffloadThreshold: 80,
		MemoryDir:        testMemDir,
		MaxSelectTools:   2,
	})

	t.Log(">>> 开始执行治理前 (Ungoverned) 多轮对话...")
	for i, p := range prompts {
		round := i + 1
		m, err := ungovernedAgent.RunTurn(ctx, round, p)
		if err != nil {
			t.Fatalf("未治理 Agent 第 %d 轮运行失败: %v", round, err)
		}
		t.Logf("[未治理 R%d] Token: %-5d | 动作: %s", round, m.InputTokens, m.ActionNote)
	}

	t.Log("\n>>> 开始执行治理后 (Governed) 多轮对话...")
	for i, p := range prompts {
		round := i + 1
		m, err := governedAgent.RunTurn(ctx, round, p)
		if err != nil {
			t.Fatalf("治理后 Agent 第 %d 轮运行失败: %v", round, err)
		}
		t.Logf("[治理后 R%d] Token: %-5d | 动作: %s", round, m.InputTokens, m.ActionNote)
	}

	// 3. 构建对比数据并输出表格与曲线
	records := BuildComparisonRecords(ungovernedAgent.GetMetrics(), governedAgent.GetMetrics())
	PrintComparisonTable(records)

	curveStr := RenderTokenCurve(records)
	fmt.Println(curveStr)

	// 4. 关键指标断言验收
	uMetrics := ungovernedAgent.GetMetrics()
	gMetrics := governedAgent.GetMetrics()

	// 验收点 A: 终态 Token 治理后必须显著低于未治理（降幅通常大于 70%）
	lastU := uMetrics[len(uMetrics)-1].InputTokens
	lastG := gMetrics[len(gMetrics)-1].InputTokens
	if lastG >= lastU {
		t.Errorf("治理后终态 Token (%d) 未低于未治理 (%d)", lastG, lastU)
	}
	reduction := float64(lastU-lastG) / float64(lastU) * 100.0
	t.Logf("终态 Token 降幅达到: %.1f%% (%d -> %d)", reduction, lastU, lastG)
	if reduction < 60.0 {
		t.Errorf("治理后 Token 降幅不足 60%%: 实际为 %.1f%%", reduction)
	}

	// 验收点 B: 治理后的增长曲线必须有界或明显放缓
	// 未治理曲线随长文档持续线性暴增；治理后历史 Token 保持在有界区间
	if gMetrics[len(gMetrics)-1].HistoryTokens > 400 {
		t.Errorf("治理后历史 Token 未能受控: 最终历史 Token = %d (预期 <= 400)", gMetrics[len(gMetrics)-1].HistoryTokens)
	}

	// 验收点 C: 任务质量验证——关键事实不得因压缩或外置而丢失
	for i, r := range records {
		if !r.FactPreserved {
			t.Errorf("第 %d 轮关键技术事实出现丢失: Prompt=%s", i+1, r.UserPrompt)
		}
	}
}
