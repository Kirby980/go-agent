package ctxeng

import (
	"fmt"
	"strings"
)

// ComparisonRecord 记录治理前后单轮对比数据
type ComparisonRecord struct {
	Round            int
	UserPrompt       string
	UngovernedTokens int
	GovernedTokens   int
	ReductionPercent float64
	GovernedAction   string
	FactPreserved    bool
}

// BuildComparisonRecords 汇总两组指标生成对比报表
func BuildComparisonRecords(ungoverned, governed []TurnMetrics) []ComparisonRecord {
	n := len(ungoverned)
	if len(governed) < n {
		n = len(governed)
	}

	records := make([]ComparisonRecord, 0, n)
	for i := 0; i < n; i++ {
		u := ungoverned[i]
		g := governed[i]

		red := 0.0
		if u.InputTokens > 0 {
			red = float64(u.InputTokens-g.InputTokens) / float64(u.InputTokens) * 100.0
			if red < 0 {
				red = 0
			}
		}

		records = append(records, ComparisonRecord{
			Round:            i + 1,
			UserPrompt:       u.UserPrompt,
			UngovernedTokens: u.InputTokens,
			GovernedTokens:   g.InputTokens,
			ReductionPercent: red,
			GovernedAction:   g.ActionNote,
			FactPreserved:    g.FactsPreserved,
		})
	}
	return records
}

// PrintComparisonTable 打印格式化 Markdown/控制台对比表格
func PrintComparisonTable(records []ComparisonRecord) {
	fmt.Println("\n================================ 📊 上下文治理前后 Token 对比表 ================================")
	fmt.Printf("%-6s | %-12s | %-12s | %-8s | %-32s | %-8s\n",
		"轮次", "未治理 Token", "治理后 Token", "Token降幅", "治理动作触发", "事实保留")
	fmt.Println(strings.Repeat("-", 95))

	for _, r := range records {
		factStr := "✅ 正确"
		if !r.FactPreserved {
			factStr = "❌ 丢失"
		}
		fmt.Printf("Round%-2d | %-12d | %-12d | %6.1f%%  | %-32s | %-8s\n",
			r.Round, r.UngovernedTokens, r.GovernedTokens, r.ReductionPercent, truncate(r.GovernedAction, 32), factStr)
	}
	fmt.Println(strings.Repeat("=", 95))
}

// RenderTokenCurve 绘制终端高保真 ASCII “轮次—Token” 变化曲线
func RenderTokenCurve(records []ComparisonRecord) string {
	if len(records) == 0 {
		return ""
	}

	maxToken := 0
	for _, r := range records {
		if r.UngovernedTokens > maxToken {
			maxToken = r.UngovernedTokens
		}
		if r.GovernedTokens > maxToken {
			maxToken = r.GovernedTokens
		}
	}
	// 向上取整到合适刻度
	if maxToken == 0 {
		maxToken = 1000
	}
	maxToken = ((maxToken / 500) + 1) * 500

	chartHeight := 10 // 图表纵坐标刻度级数
	numRounds := len(records)

	// 创建二维网格
	grid := make([][]rune, chartHeight+1)
	colWidth := 7 // 每轮横向间距
	totalWidth := numRounds * colWidth
	for row := 0; row <= chartHeight; row++ {
		grid[row] = make([]rune, totalWidth)
		for col := 0; col < totalWidth; col++ {
			grid[row][col] = ' '
		}
	}

	// 映射坐标点并打标：未治理用 '*'，治理后用 'o'，重叠用 '@'
	for i, r := range records {
		col := i*colWidth + 3

		// 计算未治理 Y
		uRow := chartHeight - int(float64(r.UngovernedTokens)/float64(maxToken)*float64(chartHeight))
		if uRow < 0 {
			uRow = 0
		}
		if uRow > chartHeight {
			uRow = chartHeight
		}

		// 计算治理后 Y
		gRow := chartHeight - int(float64(r.GovernedTokens)/float64(maxToken)*float64(chartHeight))
		if gRow < 0 {
			gRow = 0
		}
		if gRow > chartHeight {
			gRow = chartHeight
		}

		if uRow == gRow {
			grid[uRow][col] = '@'
		} else {
			grid[uRow][col] = '*'
			grid[gRow][col] = 'o'
		}
	}

	var sb strings.Builder
	sb.WriteString("\n┌──────────────────────── 📈 轮次 — Token 占用增长曲线 (ASCII) ────────────────────────┐\n")
	sb.WriteString("│ 图例说明: \033[31m* 未治理(持续单调暴涨)\033[0m   \033[32mo 治理后(预算门控+Compact+外置，趋于有界)\033[0m   @ 重叠 │\n")
	sb.WriteString("├────────────────────────────────────────────────────────────────────────────────────────┤\n")

	// 逐行绘制 Y 轴与数据点
	step := maxToken / chartHeight
	for row := 0; row <= chartHeight; row++ {
		val := maxToken - row*step
		sb.WriteString(fmt.Sprintf("│ %5d ┤ ", val))
		sb.WriteString(string(grid[row]))
		sb.WriteString(" │\n")
	}

	// 绘制 X 轴
	sb.WriteString("│       ┼")
	sb.WriteString(strings.Repeat("─", totalWidth))
	sb.WriteString("─┤\n")

	// 绘制 X 轴轮次标签
	sb.WriteString("│ 轮次  : ")
	for i := 1; i <= numRounds; i++ {
		sb.WriteString(fmt.Sprintf(" R%-2d  ", i))
	}
	sb.WriteString("  │\n")
	sb.WriteString("└────────────────────────────────────────────────────────────────────────────────────────┘\n")

	return sb.String()
}

func truncate(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes-1]) + "…"
}
