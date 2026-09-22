// Package mas (Multi-Agent System) 提供了多种经典的工业级与前沿多智能体协同模式。
//
// 本包涵盖了以下多 Agent 协作范式：
// 1. Supervisor-Worker（主管-工作者模式）：由中心化主管根据进展动态调度工人和判断完结。
// 2. Isolated Orchestrator（分治编排与上下文隔离模式）：主 Agent 拆分子任务，为每个角色派生全新上下文的子 Agent 并行求解并汇总。
// 3. Multi-Agent Debate（多智能体辩论模式）：多位不同立场的专家多轮对抗推演，由裁判统一裁决定稿。
// 4. Swarm Handoff（去中心化接力转交模式）：平级智能体自主决定给出终答或动态移交会话控制权。
// 5. Stage Pipeline（流式流水线批处理模式）：利用 Go 泛型与 Channel 实现的多 Worker 阶段级流式数据处理。
// 6. Message Bus（事件驱动消息总线模式）：基于 Pub/Sub 的点对点私信与全员广播解耦通信底座。
package mas

// Message 定义了多智能体系统中跨 Agent 异步通信的标准消息载荷。
type Message struct {
	From    string         // 发送方 Agent 名称标识
	To      string         // 接收方 Agent 名称标识（填 "*" 表示全员广播）
	Content string         // 消息正文文本（通常是自然语言指令、分析报告或数据 JSON）
	Meta    map[string]any // 附加元数据（如追踪 TraceID、轮次 Round、时间戳等扩展信息）
}
