package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Kirby980/agent/llm"
	"github.com/Kirby980/agent/prompt"
	"github.com/Kirby980/agent/router"
)

func main() {
	// Ctrl+C → context 取消
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	question := strings.Join(os.Args[1:], " ")
	if question == "" {
		fmt.Println("用法: minicall <你的问题>")
		os.Exit(1)
	}

	cfg := loadConfigFromEnv() // TODO: 读取 BASE_URL / API_KEY / MODEL
	if err := runOnce(ctx, cfg, question); err != nil {
		fmt.Fprintln(os.Stderr, "\n出错:", err)
		os.Exit(1)
	}
}

// runOnce: 构造请求 → transport.NewClient().Do(req) → 解析 choices[0].message.content 与 usage → 打印。
// TODO: 由你实现，这是本模块的综合验收。
func runOnce(ctx context.Context, cfg Config, question string) error {
	t, err := prompt.New("redis", prompt.DocAssistantTmpl)
	if err != nil {
		fmt.Println(err)
	}
	systemPrompt, err := t.Render(map[string]any{
		"Product": "redis",
		"Docs":    []string{"redis各命令是什么？", "redis的底层结果是什么？"},
	})
	if err != nil {
		fmt.Println(err)
	}
	payload := llm.ChatRequest{
		Model: "grok-4.6",
		Messages: []llm.Message{
			{
				Role:    "system",
				Content: systemPrompt,
			},
			{
				Role:    "user",
				Content: question,
			},
		},
		//Stream: true,
	}
	// body, err := json.Marshal(payload)
	// if err != nil {
	// 	return err
	// }
	p := BuildAll(cfg)
	provider := make([]llm.Provider, 0)
	for _, v := range p {
		provider = append(provider, v)
	}
	run, err := router.New(router.Priority{}, provider...)
	if err != nil {
		fmt.Println(err)
	}
	resp, name, err := run.Chat(ctx, payload)
	if err != nil {
		return err
	}
	if resp == nil {
		return nil
	}
	// for v := range resp {
	// 	fmt.Println(v, name)
	// }
	fmt.Printf("resp: %v\n name: %s", resp, name)
	// req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	// if err != nil {
	// 	return err
	// }
	// req.Header.Set("Authorization", "Bearer "+cfg.key)
	// req.Header.Set("Content-Type", "application/json")
	// client := transport.NewClient()
	// resp, err := client.Do(req)
	// if err != nil {
	// 	return err
	// }
	// defer resp.Body.Close()

	// if resp.StatusCode != http.StatusOK {
	// 	b, _ := io.ReadAll(resp.Body)
	// 	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, b)
	// }

	// // OpenAI 兼容响应的线上格式，只取用得到的字段
	// var wire struct {
	// 	Choices []struct {
	// 		Message llm.Message `json:"message"`
	// 	} `json:"choices"`
	// 	Usage struct {
	// 		PromptTokens     int `json:"prompt_tokens"`
	// 		CompletionTokens int `json:"completion_tokens"`
	// 	} `json:"usage"`
	// }
	// if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
	// 	return err
	// }
	// if len(wire.Choices) == 0 {
	// 	return fmt.Errorf("响应里没有 choices")
	// }
	// fmt.Println(wire.Choices[0].Message.Content)
	// fmt.Fprintf(os.Stderr, "\n[tokens] in=%d out=%d\n", wire.Usage.PromptTokens, wire.Usage.CompletionTokens)
	return nil
}
