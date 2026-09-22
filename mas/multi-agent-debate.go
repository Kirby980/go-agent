package mas

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Kirby980/agent/llm"
)

// Debater 表示多智能体辩论中的一位独立辩手。
type Debater struct {
	Name     string       // 辩手名称（例如 "性能专家", "安全专家", "业务架构师"）
	Provider llm.Provider // 辩手背后的模型提供商（支持使用异构模型互相校准）
	Model    string       // 辩手使用的模型名
	Persona  string       // 辩手所持的专业立场、价值取向与系统级身份设定
}

// Debate 驱动多位辩手展开指定轮次（rounds）的结构化交叉辩论，最终输出每位辩手的终轮观点。
//
// 辩论演进机制：
// 1. 第 0 轮（首轮自由发散）：各位辩手根据原始问题和各自 Persona 独立作答。
// 2. 第 1 ~ N-1 轮（交叉批判与自我修正）：
//    - 在每轮开始前为上一轮所有辩手的观点建立只读快照（prev）。
//    - 各辩手并行审视其他人的反方观点，批判性吸纳合理建议，修补漏洞并强化自己的论据。
// 3. 结果收敛：通过多轮认知碰撞，消除单模型的幻觉与盲区，最终由 mu 保护写回 answers。
func Debate(ctx context.Context, debaters []Debater, question string, rounds int) (map[string]string, error) {
	answers := make(map[string]string)
	var mu sync.Mutex

	for round := 0; round < rounds; round++ {
		// 1. 获取本轮启动前的前序答案快照，确保本轮并发读取时无数据竞争
		mu.Lock()
		prev := make(map[string]string, len(answers))
		for k, v := range answers {
			prev[k] = v
		}
		mu.Unlock()

		// 2. 并发唤醒所有辩手进入新一轮论证
		var wg sync.WaitGroup
		errCh := make(chan error, len(debaters))
		for _, d := range debaters {
			d := d
			wg.Add(1)
			go func() {
				defer wg.Done()
				// 将前序轮次中其他专家的观点汇总为输入提示词
				ans, err := chat(ctx, d.Provider, d.Model, d.Persona, debatePrompt(question, d.Name, prev))
				if err != nil {
					errCh <- err
					return
				}
				mu.Lock()
				answers[d.Name] = ans
				mu.Unlock()
			}()
		}
		wg.Wait()
		close(errCh)

		// 3. 错误检测：若本轮有辩手模型发生网络或 API 错误，立即熔断返回
		if err := <-errCh; err != nil {
			return nil, err
		}
	}
	return answers, nil
}

// debatePrompt 构造辩论提示词：
// 首轮只给原始问题；后续轮次自动注入其他所有同僚上一轮的观点，要求模型批判性反思与修订。
func debatePrompt(question, self string, prev map[string]string) string {
	if len(prev) == 0 {
		return "问题：" + question + "\n请给出你的回答和理由。"
	}
	var sb strings.Builder
	sb.WriteString("问题：" + question + "\n\n其他成员上一轮的观点：\n")
	for name, ans := range prev {
		if name == self {
			continue // 排除自己上一轮的原话
		}
		fmt.Fprintf(&sb, "- %s：%s\n", name, ans)
	}
	sb.WriteString("\n请批判性地参考他们的观点，修订并强化你的回答。")
	return sb.String()
}

// Judge 充当客观中立的终审法官/评审专家：
// 接收所有辩手在多轮辩论后的最终收敛成果，综合权衡各方立场的利弊，产出最终裁决定稿。
func Judge(ctx context.Context, p llm.Provider, model, question string, answers map[string]string) (string, error) {
	var sb strings.Builder
	for name, ans := range answers {
		fmt.Fprintf(&sb, "【%s】%s\n\n", name, ans)
	}
	return chat(ctx, p, model,
		"你是评审。综合下面各位专家的最终回答，给出最准确、全面、平衡的定稿。",
		"问题："+question+"\n\n各方回答：\n"+sb.String())
}
