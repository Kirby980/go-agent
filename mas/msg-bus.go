package mas

import (
	"context"
	"fmt"
	"sync"
)

// MessageBus 实现了用于多智能体解耦通信的内存级消息总线（Pub/Sub 模式）。
//
// 设计原则：
// 1. 邮箱隔离：每个参与协作的 Agent 拥有专属的缓冲 Channel（收件箱 inbox）。
// 2. 双模路由：支持基于名称的点对点定向通信（P2P）与全员广播（Broadcast，排除发送方自身）。
// 3. 上下文联动：所有消息发送操作感知 context.Context，支持超时熔断与主动取消。
type MessageBus struct {
	mu      sync.RWMutex
	inboxes map[string]chan Message // Agent 名称 -> 专属收件箱 Channel
	buffer  int                     // 每个收件箱 Channel 的缓冲区大小
}

// NewMessageBus 创建并初始化一个消息总线实例。
// buffer 参数指定每个 Agent 邮箱 Channel 的缓冲区深度，防范突发流量导致总线阻塞。
func NewMessageBus(buffer int) *MessageBus {
	return &MessageBus{
		inboxes: make(map[string]chan Message),
		buffer:  buffer,
		mu:      sync.RWMutex{},
	}
}

// Subscribe 为指定名称的 Agent 注册并分配一个专属收件箱，返回只读 channel 供其消费。
func (b *MessageBus) Subscribe(name string) <-chan Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan Message, b.buffer)
	b.inboxes[name] = ch
	return ch
}

// Publish 把消息投递至目标收件箱：
// 1. 若 msg.To 指定了具体成员名且非 "*"，则精准路由至该成员的收件箱。
// 2. 若 msg.To 为空或为 "*"，则广播投递给当前已注册的所有成员（自动跳过发送方自己）。
func (b *MessageBus) Publish(ctx context.Context, msg Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// 辅助闭包：向目标通道投递消息，感知上下文取消
	send := func(ch chan Message) error {
		select {
		case ch <- msg:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// 点对点定向投递
	if msg.To != "" && msg.To != "*" {
		ch, ok := b.inboxes[msg.To]
		if !ok {
			return fmt.Errorf("收件人不存在: %s", msg.To)
		}
		return send(ch)
	}

	// 广播投递给除自身外的所有活跃 Agent
	for name, ch := range b.inboxes {
		if name == msg.From {
			continue
		}
		if err := send(ch); err != nil {
			return err
		}
	}
	return nil
}

// Close 关闭总线上所有注册的收件箱 Channel，通知所有监听的 Agent 退出消费。
func (b *MessageBus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.inboxes {
		close(ch)
	}
	b.inboxes = make(map[string]chan Message)
}

// RunBusAgent 启动一个基于消息总线的异步事件循环处理协程：
// 1. 自动为名为 name 的 Agent 订阅收件箱；
// 2. 在后台 Goroutine 中持续监听收到的消息；
// 3. 调用 handle 回调函数处理消息，若 handle 返回了新的回复消息，自动通过总线发布出去；
// 4. 当 ctx 取消或总线关闭时优雅退出。
func RunBusAgent(ctx context.Context, bus *MessageBus, name string, handle func(context.Context, Message) (*Message, error)) {
	inbox := bus.Subscribe(name)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-inbox:
				if !ok {
					return // 总线通道已关闭，优雅退出
				}
				reply, err := handle(ctx, msg)
				if err != nil || reply == nil {
					continue
				}
				_ = bus.Publish(ctx, *reply)
			}
		}
	}()
}
