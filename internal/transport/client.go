package transport

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Do 执行请求并按需重试 429/5xx（指数退避 + 抖动，优先尊重 Retry-After）。
// 签名刻意对齐标准库 http.Client.Do：ctx 通过 http.NewRequestWithContext 装进请求里。
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx); err != nil { // 限流：等到有令牌，或被 ctx 取消
			return nil, err
		}
	}
	var lastErr error
	for attempt := 0; attempt <= c.retry.MaxRetries; attempt++ {
		// 重试前重建一次性的 Body —— 否则 POST 重试会发空体。
		// http.NewRequest 对 bytes/strings reader 会自动设好 GetBody，这里直接用。
		if req.Body != nil && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			req.Body = body
		}
		resp, err := c.http.Do(req)
		var wait time.Duration
		switch {
		case err != nil:
			lastErr = err // 网络层错误，可重试
			wait = backoff(attempt, c.retry)
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			wait = retryAfter(resp) // 优先尊重服务端的 Retry-After
			if wait <= 0 {
				wait = backoff(attempt, c.retry)
			}
			resp.Body.Close()
			lastErr = fmt.Errorf("服务端返回 %d", resp.StatusCode)
		default:
			return resp, nil // 2xx/4xx(非429) 直接返回，交给上层处理
		}

		if attempt == c.retry.MaxRetries { // 最后一次仍失败：不再等待，直接退出
			break
		}
		if !sleep(ctx, wait) {
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("重试 %d 次后仍失败: %w", c.retry.MaxRetries, lastErr)
}

// backoff 计算第 attempt 次重试的等待时长：指数增长 + 抖动，且不超过 MaxDelay。
func backoff(attempt int, cfg RetryConfig) time.Duration {
	d := cfg.BaseDelay << attempt // 0.5s, 1s, 2s, 4s...
	if d > cfg.MaxDelay {
		d = cfg.MaxDelay
	}
	// 加抖动，让结果落在约 0.9~1.1 倍之间（±10%），避免大量客户端同时重试造成“惊群”
	jitter := time.Duration(rand.Int63n(int64(d) / 5))
	return d - (d / 10) + jitter
}

// retryAfter 解析 429/503 响应里的 Retry-After 头：支持「秒数」与「HTTP-date」两种形式。
func retryAfter(resp *http.Response) time.Duration {
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0
	}
	if sec, err := strconv.Atoi(v); err == nil { // 形式一：延迟秒数，如 "3"
		return time.Duration(sec) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil { // 形式二：HTTP-date
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// sleep 在等待 d 的同时监听 ctx；被取消则返回 false。
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// ParseSSE 按空行聚合一个完整 SSE 事件，再把该事件的 data 交给 onData。
// 同一事件可以包含多行 data，规范要求使用换行符连接，不能逐行回调。
// onData 返回非 nil error 时提前终止；读到 EOF 时结束。
// [DONE]、message_stop 等协议结束标记由各 Provider 解析，通用层不擅自吞掉。
func ParseSSE(r io.Reader, onData func(data []byte) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var dataLines []string
	dispatch := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		return onData([]byte(data))
	}
	for scanner.Scan() {
		// SSE协议末尾以\n 或者\r\n结束，去除\r
		line := strings.TrimSuffix(scanner.Text(), "\r")
		// SSE以空行结束，所以空行才触发匿名函数
		if line == "" {
			if err := dispatch(); err != nil {
				return err
			}
			continue
		}
		// SSE 注释/心跳
		if strings.HasPrefix(line, ":") {
			continue
		}

		field, value, found := strings.Cut(line, ":")
		// 如果没有两个字段，则只是field value为空
		if !found {
			field, value = line, ""
		} else {
			value = strings.TrimPrefix(value, " ")
		}
		if field == "data" {
			dataLines = append(dataLines, value)
		}
	}
	// SSE 规范规定：连接在空行前结束时，未完成的事件不派发。
	return scanner.Err()
}
