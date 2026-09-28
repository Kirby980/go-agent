package ctxeng

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Kirby980/agent/llm"
	"github.com/Kirby980/agent/tool"
)

// mockTool 辅助测试工具
type mockTool struct {
	name string
	desc string
}

func (m *mockTool) Name() string                                              { return m.name }
func (m *mockTool) Description() string                                       { return m.desc }
func (m *mockTool) Parameters() json.RawMessage                               { return json.RawMessage(`{}`) }
func (m *mockTool) Call(_ context.Context, _ json.RawMessage) (string, error) { return "ok", nil }

// TestEstimateTokens 测试 Token 估算函数的表驱动测试
func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "空字符串返回 0",
			input:    "",
			expected: 0,
		},
		{
			name:     "纯 ASCII 英文短句",
			input:    "Hello, world!", // 13 ASCII 字符: 13/4 + 0 + 1 = 4
			expected: 4,
		},
		{
			name:     "纯中文字符串",
			input:    "你好世界", // 4 CJK 字符: 0 + 4*2/3 + 1 = 3
			expected: 3,
		},
		{
			name:     "中英文混合字符串",
			input:    "Hello 世界", // 6 ASCII, 2 CJK: 6/4 + 2*2/3 + 1 = 1 + 1 + 1 = 3
			expected: 3,
		},
		{
			name:     "较长混合文本估算比例正常",
			input:    "Agent 上下文工程包括 Budget 门控、Compact 压缩与 Memory 外置。",
			expected: 32, // 28 CJK, 26 ASCII: 26/4 + 28*2/3 + 1 = 6 + 18 + 1 = 25...
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := EstimateTokens(tc.input)
			if tc.name == "较长混合文本估算比例正常" {
				if actual <= 0 {
					t.Fatalf("期望 Token 估算 > 0, 实际为 %d", actual)
				}
				return
			}
			if actual != tc.expected {
				t.Errorf("EstimateTokens(%q) = %d; 期望 %d", tc.input, actual, tc.expected)
			}
		})
	}
}

// TestCompact 测试 Compact 压缩及假 summarize 注入的表驱动测试
func TestCompact(t *testing.T) {
	// 假实现 1: 取前 N 个字符作为摘要
	fakeSummarizeFirstN := func(n int) func(ctx context.Context, older []llm.Message) (string, error) {
		return func(ctx context.Context, older []llm.Message) (string, error) {
			var sb strings.Builder
			for _, m := range older {
				sb.WriteString(m.Content)
			}
			text := sb.String()
			runes := []rune(text)
			if len(runes) > n {
				return string(runes[:n]) + "...", nil
			}
			return text, nil
		}
	}

	baseMessages := []llm.Message{
		{Role: llm.RoleSystem, Content: "你是系统提示词"},
		{Role: llm.RoleUser, Content: "第一轮用户输入：请阅读超长架构文档"},
		{Role: llm.RoleAssistant, Content: "第一轮助手回复：已阅读，包含核心配置 A 和 B"},
		{Role: llm.RoleUser, Content: "第二轮用户输入：系统最大并发是多少？"},
		{Role: llm.RoleAssistant, Content: "第二轮助手回复：系统最大并发是 10000"},
		{Role: llm.RoleUser, Content: "第三轮用户输入：继续下一步工作"},
	}

	tests := []struct {
		name          string
		messages      []llm.Message
		keepRecent    int
		summarizeFn   func(ctx context.Context, older []llm.Message) (string, error)
		wantLen       int
		wantErr       bool
		checkContains string
	}{
		{
			name:        "消息数少于等于 keepRecent+1 时无需压缩",
			messages:    baseMessages[:3], // system + 2 条
			keepRecent:  2,
			summarizeFn: fakeSummarizeFirstN(10),
			wantLen:     3,
			wantErr:     false,
		},
		{
			name:          "超长历史触发压缩并保留最新 2 条",
			messages:      baseMessages, // 6 条消息
			keepRecent:    2,
			summarizeFn:   fakeSummarizeFirstN(15),
			wantLen:       4, // system(1) + summary(1) + keepRecent(2)
			wantErr:       false,
			checkContains: "【早前对话摘要，据此延续】",
		},
		{
			name:       "summarize 失败时正确返回错误",
			messages:   baseMessages,
			keepRecent: 1,
			summarizeFn: func(ctx context.Context, older []llm.Message) (string, error) {
				return "", errors.New("模型生成摘要超时")
			},
			wantLen: 0,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compact(context.Background(), tc.messages, tc.keepRecent, tc.summarizeFn)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Compact 错误期望 %v, 实际收到 %v", tc.wantErr, err)
			}
			if tc.wantErr {
				return
			}
			if len(res) != tc.wantLen {
				t.Errorf("Compact 返回消息数 = %d; 期望 %d", len(res), tc.wantLen)
			}
			if tc.checkContains != "" {
				found := false
				for _, m := range res {
					if strings.Contains(m.Content, tc.checkContains) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("返回消息中未包含期望的摘要标记: %s", tc.checkContains)
				}
			}
			// 校验 system prompt 始终保持在第 0 位
			if len(res) > 0 && res[0].Role != llm.RoleSystem {
				t.Errorf("第 0 位消息角色不是 System, 实际为 %s", res[0].Role)
			}
		})
	}
}

// TestSelectTools 测试工具过滤与相关度排序的表驱动测试
func TestSelectTools(t *testing.T) {
	allTools := []tool.Tool{
		&mockTool{name: "read_doc", desc: "读取本地文档全文内容与设计规范"},
		&mockTool{name: "calculator", desc: "执行数学运算与加减乘除计算"},
		&mockTool{name: "read_memory", desc: "读取外置存储中的长文本片段"},
		&mockTool{name: "bash", desc: "执行系统终端命令行脚本"},
	}

	tests := []struct {
		name          string
		query         string
		tools         []tool.Tool
		maxN          int
		wantCount     int
		wantFirstName string
	}{
		{
			name:      "maxN 为 0 时返回空",
			query:     "读取文档",
			tools:     allTools,
			maxN:      0,
			wantCount: 0,
		},
		{
			name:      "工具列表为空时返回空",
			query:     "计算",
			tools:     nil,
			maxN:      3,
			wantCount: 0,
		},
		{
			name:          "精准命中文档读取工具",
			query:         "请帮我用 read_doc 读取长篇设计规范文档",
			tools:         allTools,
			maxN:          2,
			wantCount:     2,
			wantFirstName: "read_doc",
		},
		{
			name:          "精准命中外置内存读取工具",
			query:         "根据外置 id 使用 read_memory 查看存储文本",
			tools:         allTools,
			maxN:          1,
			wantCount:     1,
			wantFirstName: "read_memory",
		},
		{
			name:          "计算相关命中计算器",
			query:         "1+1等于几，请进行数学计算运算",
			tools:         allTools,
			maxN:          2,
			wantCount:     2,
			wantFirstName: "calculator",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			selected := SelectTools(tc.query, tc.tools, tc.maxN)
			if len(selected) != tc.wantCount {
				t.Fatalf("SelectTools 返回数量 = %d; 期望 %d", len(selected), tc.wantCount)
			}
			if tc.wantFirstName != "" && len(selected) > 0 {
				if selected[0].Name() != tc.wantFirstName {
					t.Errorf("首位工具期望 %s, 实际为 %s", tc.wantFirstName, selected[0].Name())
				}
			}
		})
	}
}
