package main

import (
	"testing"
)

func TestTruncateWidth(t *testing.T) {
	// ASCII 测试
	s1 := "hello world"
	if got := truncateWidth(s1, 5); got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}

	// 中文字符测试（每个中文字符宽度为 2）
	s2 := "打印帮助信息与支持的指令列表"
	// 截取 8 宽 -> 4 个中文字符
	if got := truncateWidth(s2, 8); got != "打印帮助" {
		t.Errorf("expected '打印帮助', got %q", got)
	}

	// 不足一个中文字符宽度时不截断半个字符
	if got := truncateWidth(s2, 9); got != "打印帮助" {
		t.Errorf("expected '打印帮助', got %q", got)
	}

	// 完整宽度不截断
	if got := truncateWidth(s2, 100); got != s2 {
		t.Errorf("expected %q, got %q", s2, got)
	}
}

func TestVisualWidthAndQuestionMarks(t *testing.T) {
	// 英文半角问号占用 1 列视觉列宽
	if w := runeWidth('?'); w != 1 {
		t.Errorf("expected '?' width to be 1, got %d", w)
	}

	// 中文全角问号占用 2 列视觉列宽（避免光标落在字符内部发生闪回）
	if w := runeWidth('？'); w != 2 {
		t.Errorf("expected '？' width to be 2, got %d", w)
	}

	// 汉字占用 2 列
	if w := runeWidth('你'); w != 2 {
		t.Errorf("expected '你' width to be 2, got %d", w)
	}

	// 完整中英混合字符串视觉宽度计算
	// "hello?" -> 6 列
	if sw := stringWidth("hello?"); sw != 6 {
		t.Errorf("expected stringWidth('hello?') = 6, got %d", sw)
	}

	// "你好吗？" -> 4个全角字符 = 8 列
	if sw := stringWidth("你好吗？"); sw != 8 {
		t.Errorf("expected stringWidth('你好吗？') = 8, got %d", sw)
	}
}

func TestCleanANSI(t *testing.T) {
	colored := "\033[36magent\033[0m (\033[33mdefault\033[0m) > "
	clean := cleanANSI(colored)
	expected := "agent (default) > "
	if clean != expected {
		t.Errorf("expected %q, got %q", expected, clean)
	}
}

func TestWindowScrollAndBoundaryNoWrap(t *testing.T) {
	// 模拟 10 个指令列表，视口大小为 6
	totalItems := 10
	maxShow := 6

	selectedIdx := 0
	scrollStart := 0

	updateViewport := func() (int, int) {
		if totalItems <= maxShow {
			scrollStart = 0
		} else {
			if selectedIdx < scrollStart {
				scrollStart = selectedIdx
			} else if selectedIdx >= scrollStart+maxShow {
				scrollStart = selectedIdx - maxShow + 1
			}
			if scrollStart < 0 {
				scrollStart = 0
			}
			if scrollStart+maxShow > totalItems {
				scrollStart = totalItems - maxShow
			}
		}
		start := scrollStart
		end := start + maxShow
		if end > totalItems {
			end = totalItems
		}
		return start, end
	}

	// 初始状态：选中 0，窗口 [0, 6)
	start, end := updateViewport()
	if start != 0 || end != 6 || selectedIdx != 0 {
		t.Fatalf("unexpected init: start=%d end=%d sel=%d", start, end, selectedIdx)
	}

	// 模拟向下按键 5 次，选中移动到 5 (视口最后一条)
	for i := 0; i < 5; i++ {
		if selectedIdx < totalItems-1 {
			selectedIdx++
		}
		start, end = updateViewport()
	}
	if selectedIdx != 5 || start != 0 || end != 6 {
		t.Fatalf("expected sel=5, start=0, end=6; got sel=%d start=%d end=%d", selectedIdx, start, end)
	}

	// 再向下 1 次：选中 6，窗口应该平滑滚动为 [1, 7)
	if selectedIdx < totalItems-1 {
		selectedIdx++
	}
	start, end = updateViewport()
	if selectedIdx != 6 || start != 1 || end != 7 {
		t.Fatalf("expected sel=6, start=1, end=7; got sel=%d start=%d end=%d", selectedIdx, start, end)
	}

	// 继续向下直到到达末尾 (第 9 项)
	for i := 0; i < 10; i++ {
		if selectedIdx < totalItems-1 {
			selectedIdx++
		}
		start, end = updateViewport()
	}
	if selectedIdx != 9 || start != 4 || end != 10 {
		t.Fatalf("expected at bottom sel=9, start=4, end=10; got sel=%d start=%d end=%d", selectedIdx, start, end)
	}

	// 核心测试：到达底部后继续按向下键，不应无限循环回顶部！必须停在底部 9！
	for i := 0; i < 3; i++ {
		if selectedIdx < totalItems-1 {
			selectedIdx++
		}
		start, end = updateViewport()
	}
	if selectedIdx != 9 {
		t.Fatalf("expected to stay at bottom 9, but got sel=%d", selectedIdx)
	}

	// 模拟向上按键 3 次：选中从 9 变为 6，窗口 [4, 10) 保持固定，高亮光标在固定窗口内上移
	for i := 0; i < 3; i++ {
		if selectedIdx > 0 {
			selectedIdx--
		}
		start, end = updateViewport()
	}
	if selectedIdx != 6 || start != 4 || end != 10 {
		t.Fatalf("expected sel=6, start=4, end=10 (fixed window); got sel=%d start=%d end=%d", selectedIdx, start, end)
	}

	// 继续向上直到第 0 项
	for i := 0; i < 10; i++ {
		if selectedIdx > 0 {
			selectedIdx--
		}
		start, end = updateViewport()
	}
	if selectedIdx != 0 || start != 0 || end != 6 {
		t.Fatalf("expected at top sel=0, start=0, end=6; got sel=%d start=%d end=%d", selectedIdx, start, end)
	}

	// 核心测试：到达顶部后继续按向上键，不应无限循环到底部！必须停在顶部 0！
	for i := 0; i < 3; i++ {
		if selectedIdx > 0 {
			selectedIdx--
		}
		start, end = updateViewport()
	}
	if selectedIdx != 0 {
		t.Fatalf("expected to stay at top 0, but got sel=%d", selectedIdx)
	}
}

func TestMatchesDisambiguation(t *testing.T) {
	ed := NewLineEditor("prompt> ", "", defaultSlashCommands)

	getMatches := func(str string) []CommandItem {
		if len(str) == 0 || str[0] != '/' || containsAny(str, " ") || containsAny(str[1:], "/") {
			return nil
		}
		var res []CommandItem
		for _, c := range ed.commands {
			if len(c.Cmd) >= len(str) && c.Cmd[:len(str)] == str {
				res = append(res, c)
			}
		}
		return res
	}

	// 1. 输入单个 '/'：应展示所有指令
	mRoot := getMatches("/")
	if len(mRoot) != len(defaultSlashCommands) {
		t.Fatalf("expected %d commands, got %d", len(defaultSlashCommands), len(mRoot))
	}

	// 2. 绝对路径 /home/hyz/...：应返回 nil，避免误判
	mPath := getMatches("/home/hyz/project/file.go")
	if len(mPath) != 0 {
		t.Fatalf("expected 0 matches for path, got %d", len(mPath))
	}

	// 3. 前缀过滤 /m：应只匹配 /model
	mM := getMatches("/m")
	if len(mM) != 1 || mM[0].Cmd != "/model" {
		t.Fatalf("expected [/model], got %+v", mM)
	}

	// 4. 含空格 /help me：应返回 nil
	mSpace := getMatches("/help me")
	if len(mSpace) != 0 {
		t.Fatalf("expected 0 matches when space is present, got %d", len(mSpace))
	}
}

func containsAny(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
