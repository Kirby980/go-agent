package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type Calculator struct {
	Expr string `json:"expr"`
}

func (c *Calculator) Name() string {
	return "calculator"
}

func (c *Calculator) Description() string {
	return "计算任意合法的数学表达式并返回结果"
}

func (c *Calculator) Parameters() json.RawMessage {
	return []byte(`{
		"type": "object",
		"properties": {
			"expr": {"type": "string", "description": "要计算的数学表达式"}
		},
		"required": ["expr"]
	}`)
}

func (c *Calculator) Call(ctx context.Context, args json.RawMessage) (string, error) {
	// 这里你可以直接用 strconv.Atoi 或者你自己的计算库
	// 先简单用公式简化版，实际生产建议用 golang.org/x/exp/slices 或现成 math 包
	var params struct {
		Expr string `json:"expr"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	// 为了演示，这里用一个极简的计算器（支持 + - * / 和数字）
	result, err := CalculateSimple(params.Expr)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("计算结果: %v", result), nil
}

// 极简计算器（实际项目建议用更成熟的方案，比如 go-calc 或自定义解析器）
func CalculateSimple(expr string) (float64, error) {
	// 极简解析：支持单一二元运算 a+b, a-b, a*b, a/b，忽略空格。
	// 生产代码请用成熟解析库，本函数只满足示例场景。
	s := strings.ReplaceAll(expr, " ", "")
	if s == "" {
		return 0, fmt.Errorf("空表达式")
	}
	// 如果没有运算符，尝试直接解析为数字
	if !strings.ContainsAny(s, "+-*/") {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("无法解析数字: %w", err)
		}
		return v, nil
	}
	// 找到第一个二元运算符（忽略开头可能的负号）
	var op rune
	var idx int = -1
	for i, r := range s {
		if r == '+' || r == '-' || r == '*' || r == '/' {
			if i == 0 && r == '-' { // 这是数字的负号，不当作二元运算符
				continue
			}
			op = r
			idx = i
			break
		}
	}
	if idx == -1 {
		return 0, fmt.Errorf("未找到运算符")
	}
	left := s[:idx]
	right := s[idx+1:]
	if left == "" {
		left = "0"
	}
	if right == "" {
		return 0, fmt.Errorf("右操作数为空")
	}
	a, err := strconv.ParseFloat(left, 64)
	if err != nil {
		return 0, fmt.Errorf("左操作数解析失败: %w", err)
	}
	b, err := strconv.ParseFloat(right, 64)
	if err != nil {
		return 0, fmt.Errorf("右操作数解析失败: %w", err)
	}
	switch op {
	case '+':
		return a + b, nil
	case '-':
		return a - b, nil
	case '*':
		return a * b, nil
	case '/':
		if b == 0 {
			return 0, fmt.Errorf("除以零")
		}
		return a / b, nil
	default:
		return 0, fmt.Errorf("不支持的运算符: %c", op)
	}
}
