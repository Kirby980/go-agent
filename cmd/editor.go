package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/chzyer/readline"
)

// CommandItem 表示一个斜杠指令定义及其说明
type CommandItem struct {
	Cmd  string
	Desc string
}

// defaultSlashCommands 系统默认支持的斜杠指令列表
var defaultSlashCommands = []CommandItem{
	{Cmd: "/help", Desc: "打印帮助信息与支持的指令列表"},
	{Cmd: "/new", Desc: "开启全新对话会话 (重置当前记忆)"},
	{Cmd: "/resume", Desc: "恢复指定名称的历史会话记忆"},
	{Cmd: "/sessions", Desc: "列出本地保存的所有历史会话记录"},
	{Cmd: "/model", Desc: "查看当前活动模型或切换新模型"},
	{Cmd: "/config", Desc: "查看当前生效的各项运行配置与 RAG 状态"},
	{Cmd: "/stats", Desc: "查看当前会话 Token 用量与缓存命中率"},
	{Cmd: "/skills", Desc: "查看当前已加载的扩展技能清单"},
	{Cmd: "/team", Desc: "启动 Supervisor 多智能体团队协同执行复杂任务"},
	{Cmd: "/clear", Desc: "清空当前控制台屏幕"},
	{Cmd: "/exit", Desc: "退出 CLI 程序"},
}

// LineEditor 实现了类似 Claude Code / AGY 体验的交互式行编辑器：
// 1. 在空行输入 '/' 时，无需回车即可在光标下方实时弹出命令建议列表；
// 2. 支持通过 ↑ / ↓（及 Tab）在建议项之间快速切换并高亮预览；
// 3. 回车立即选中并确认执行高亮指令；
// 4. 输入字符实时过滤（如输入 '/m' 仅展示 '/model'）；退格删除 '/' 建议框即刻消失；
// 5. 采用内存双缓冲（Double Buffering）单次刷新与 bufio.Reader 逐 Rune 解码，彻底根除闪烁与中文/标点（如 '？'）错位。
type LineEditor struct {
	prompt      string
	historyFile string
	history     []string
	commands    []CommandItem
	reader      *bufio.Reader
}

// NewLineEditor 创建终端行编辑器
func NewLineEditor(prompt, historyFile string, cmds []CommandItem) *LineEditor {
	if len(cmds) == 0 {
		cmds = defaultSlashCommands
	}
	ed := &LineEditor{
		prompt:      prompt,
		historyFile: historyFile,
		commands:    cmds,
		reader:      bufio.NewReader(os.Stdin),
	}
	ed.loadHistory()
	return ed
}

func (ed *LineEditor) SetPrompt(p string) {
	ed.prompt = p
}

func (ed *LineEditor) loadHistory() {
	if ed.historyFile == "" {
		return
	}
	data, err := os.ReadFile(ed.historyFile)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			ed.history = append(ed.history, l)
		}
	}
}

func (ed *LineEditor) saveHistory(line string) {
	if line == "" || ed.historyFile == "" {
		return
	}
	ed.history = append(ed.history, line)
	f, err := os.OpenFile(ed.historyFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}

// Readline 启动交互式行读取循环
func (ed *LineEditor) Readline() (string, error) {
	fd := int(os.Stdin.Fd())
	oldState, err := readline.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = readline.Restore(fd, oldState)
	}()

	if ed.reader == nil {
		ed.reader = bufio.NewReader(os.Stdin)
	}

	var input []rune
	cursor := 0
	historyIdx := len(ed.history)
	savedCurrent := ""

	selectedIdx := 0
	scrollStart := 0
	prevMenuLines := 0

	// 动态计算当前匹配的 Slash Commands
	getMatches := func() []CommandItem {
		s := string(input)
		// 仅当以 '/' 开头、且不含空格、且不含二级路径 '/'（如 /home/...）时才视为正在输入指令
		if len(s) == 0 || s[0] != '/' || strings.Contains(s, " ") || strings.Contains(s[1:], "/") {
			return nil
		}
		var res []CommandItem
		for _, c := range ed.commands {
			if strings.HasPrefix(c.Cmd, s) {
				res = append(res, c)
			}
		}
		return res
	}

	// 增量无闪烁重绘（所有控制码一次性刷入终端）
	render := func() {
		var b bytes.Buffer

		matches := getMatches()
		hasMatches := len(matches) > 0

		// 1. 行清理：
		// 若前序有下拉菜单或者当前需要画下拉菜单，向下抹除 (\r\033[J)；
		// 若上一次和当前均处于普通纯文本输入状态，仅清除当前行 (\r\033[K)，彻底消除屏幕闪烁！
		if prevMenuLines > 0 || hasMatches {
			b.WriteString("\r\033[J")
		} else {
			b.WriteString("\r\033[K")
		}

		// 2. 输出 Prompt 及当前用户输入的字符
		b.WriteString(ed.prompt)
		b.WriteString(string(input))

		linesDrawn := 1

		if hasMatches {
			if selectedIdx >= len(matches) {
				selectedIdx = len(matches) - 1
			}
			if selectedIdx < 0 {
				selectedIdx = 0
			}

			// 固定窗口高度：最多显示 6 项
			maxShow := 6
			if len(matches) <= maxShow {
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
				if scrollStart+maxShow > len(matches) {
					scrollStart = len(matches) - maxShow
				}
			}

			start := scrollStart
			end := start + maxShow
			if end > len(matches) {
				end = len(matches)
			}

			termWidth := readline.GetScreenWidth()
			if termWidth <= 0 {
				termWidth = 80
			}
			maxDescWidth := termWidth - 18 // 4 (缩进) + 10 (指令名) + 3 (" - ") + 1 (边距)
			if maxDescWidth < 10 {
				maxDescWidth = 10
			}

			for i := start; i < end; i++ {
				m := matches[i]
				desc := truncateWidth(m.Desc, maxDescWidth)
				if i == selectedIdx {
					fmt.Fprintf(&b, "\r\n\033[36m  ❯ \033[1;32m%-10s\033[0m \033[90m- %s\033[0m", m.Cmd, desc)
				} else {
					fmt.Fprintf(&b, "\r\n    \033[37m%-10s\033[0m \033[90m- %s\033[0m", m.Cmd, desc)
				}
				linesDrawn++
			}

			// 将终端光标从菜单最下方精准移回 Prompt 输入行
			fmt.Fprintf(&b, "\033[%dA\r", linesDrawn-1)
			// 根据精确的字符视觉列宽（East Asian Width）计算光标位置
			targetCol := stringWidth(cleanANSI(ed.prompt)) + stringWidth(string(input[:cursor]))
			if targetCol > 0 {
				fmt.Fprintf(&b, "\033[%dC", targetCol)
			}
			prevMenuLines = linesDrawn - 1
		} else {
			prevMenuLines = 0
			// 纯文本输入状态：
			// 当前光标就在 string(input) 的真实物理末尾处！
			// 若 cursor == len(input)，无需任何光标位移，光标自然就停在末尾（绝不跳回行首再飞回，彻底解决“闪回去”！）；
			// 仅当 cursor < len(input)（即用户向左回退编辑中间内容）时，才向左回退差额列数。
			if cursor < len(input) {
				moveLeft := stringWidth(string(input[cursor:]))
				if moveLeft > 0 {
					fmt.Fprintf(&b, "\033[%dD", moveLeft)
				}
			}
		}

		// 单次 write 系统调用刷入终端，彻底消除多段输出引发的撕裂和延迟
		_, _ = os.Stdout.Write(b.Bytes())
	}

	render()

	for {
		r, _, err := ed.reader.ReadRune()
		if err != nil {
			if err == io.EOF {
				return "", io.EOF
			}
			break
		}

		matches := getMatches()
		hasMatches := len(matches) > 0

		// 处理 ANSI 转义序列（方向键等）
		if r == 27 {
			if ed.reader.Buffered() > 0 {
				b2, err := ed.reader.ReadByte()
				if err == nil && b2 == '[' {
					b3, err := ed.reader.ReadByte()
					if err == nil {
						switch b3 {
						case 'A': // Up Arrow (上方向键)
							if hasMatches {
								// 在建议菜单中向上移动（到达顶部停止，不循环）
								if selectedIdx > 0 {
									selectedIdx--
									render()
								}
							} else {
								// 历史记录向上浏览
								if historyIdx > 0 {
									if historyIdx == len(ed.history) {
										savedCurrent = string(input)
									}
									historyIdx--
									input = []rune(ed.history[historyIdx])
									cursor = len(input)
									selectedIdx = 0
									scrollStart = 0
									render()
								}
							}
						case 'B': // Down Arrow (下方向键)
							if hasMatches {
								// 在建议菜单中向下移动（到达底部停止，不循环）
								if selectedIdx < len(matches)-1 {
									selectedIdx++
									render()
								}
							} else {
								// 历史记录向下浏览
								if historyIdx < len(ed.history) {
									historyIdx++
									if historyIdx == len(ed.history) {
										input = []rune(savedCurrent)
									} else {
										input = []rune(ed.history[historyIdx])
									}
									cursor = len(input)
									selectedIdx = 0
									scrollStart = 0
									render()
								}
							}
						case 'C': // Right Arrow (光标右移)
							if cursor < len(input) {
								cursor++
								render()
							}
						case 'D': // Left Arrow (光标左移)
							if cursor > 0 {
								cursor--
								render()
							}
						case 'H': // Home 键
							cursor = 0
							render()
						case 'F': // End 键
							cursor = len(input)
							render()
						case '3': // Delete 键 (\x1b[3~)
							if ed.reader.Buffered() > 0 {
								if next, _ := ed.reader.Peek(1); len(next) > 0 && next[0] == '~' {
									_, _ = ed.reader.ReadByte()
								}
							}
							if cursor < len(input) {
								input = append(input[:cursor], input[cursor+1:]...)
								selectedIdx = 0
								scrollStart = 0
								render()
							}
						}
						continue
					}
				}
			}
			// 独立的 ESC 键：若建议菜单激活则重绘消除，否则忽略
			if hasMatches {
				render()
			}
			continue
		}

		switch r {
		case '\r', '\n':
			// 回车键：若建议框激活则采纳当前选中的指令，否则提交当前输入行
			var finalStr string
			if hasMatches && len(matches) > 0 {
				finalStr = matches[selectedIdx].Cmd
			} else {
				finalStr = string(input)
			}

			// 清空所有下拉行，回到行首输出完整结果并换行
			var endBuf bytes.Buffer
			if prevMenuLines > 0 {
				fmt.Fprint(&endBuf, "\r\033[J")
			} else {
				fmt.Fprint(&endBuf, "\r\033[K")
			}
			fmt.Fprintf(&endBuf, "%s%s\r\n", ed.prompt, finalStr)
			_, _ = os.Stdout.Write(endBuf.Bytes())

			trimmed := strings.TrimSpace(finalStr)
			if trimmed != "" {
				ed.saveHistory(trimmed)
			}
			return trimmed, nil

		case 3: // Ctrl+C: 放弃当前行
			var endBuf bytes.Buffer
			if prevMenuLines > 0 {
				fmt.Fprint(&endBuf, "\r\033[J")
			} else {
				fmt.Fprint(&endBuf, "\r\033[K")
			}
			fmt.Fprintf(&endBuf, "%s%s^C\r\n", ed.prompt, string(input))
			_, _ = os.Stdout.Write(endBuf.Bytes())
			return "", readline.ErrInterrupt

		case 4: // Ctrl+D: 行空时退出程序 (EOF)
			if len(input) == 0 {
				var endBuf bytes.Buffer
				if prevMenuLines > 0 {
					fmt.Fprint(&endBuf, "\r\033[J\r\n")
				} else {
					fmt.Fprint(&endBuf, "\r\033[K\r\n")
				}
				_, _ = os.Stdout.Write(endBuf.Bytes())
				return "", io.EOF
			}
			// 类似 Delete 删除当前光标字符
			if cursor < len(input) {
				input = append(input[:cursor], input[cursor+1:]...)
				selectedIdx = 0
				scrollStart = 0
				render()
			}

		case 127, 8: // Backspace 退格键
			if cursor > 0 {
				input = append(input[:cursor-1], input[cursor:]...)
				cursor--
				selectedIdx = 0
				scrollStart = 0
				render()
			}

		case '\t': // Tab 键：将当前建议自动补全到行中
			if hasMatches && len(matches) > 0 {
				chosen := matches[selectedIdx].Cmd
				input = []rune(chosen)
				cursor = len(input)
				selectedIdx = 0
				scrollStart = 0
				render()
			}

		case 1: // Ctrl+A (回到行首)
			cursor = 0
			render()

		case 5: // Ctrl+E (去往行尾)
			cursor = len(input)
			render()

		case 11: // Ctrl+K (剪切至行尾)
			input = input[:cursor]
			selectedIdx = 0
			scrollStart = 0
			render()

		case 21: // Ctrl+U (剪切至行首)
			input = input[cursor:]
			cursor = 0
			selectedIdx = 0
			scrollStart = 0
			render()

		default:
			// 可打印字符（完美支持英文字符、数字、全角中文符号包括'？'、中文汉字、Emoji 等）
			if r >= 32 && r != 127 {
				input = append(input[:cursor], append([]rune{r}, input[cursor:]...)...)
				cursor++
				selectedIdx = 0
				scrollStart = 0
				// 若缓冲区中还有待处理输入（如用户粘贴文本或快速连击），暂缓渲染直到缓冲区读空，极致提速
				if ed.reader.Buffered() == 0 {
					render()
				}
			}
		}
	}

	return string(input), nil
}

// runeWidth 计算单个 Unicode 字符在终端屏幕上所占用的视觉列宽 (East Asian Width)
func runeWidth(r rune) int {
	if r < 32 || (r >= 0x7f && r < 0xa0) {
		return 0
	}
	if r < 127 {
		return 1
	}
	// 涵盖全角标点区 0xFF00~0xFF60（包括全角中文问号 '？' U+FF1F、感叹号 '！' 等）
	// 以及汉字、日韩统一表意文字等，在终端中均占用 2 个视觉列宽
	if (r >= 0x1100 && r <= 0x115F) ||
		(r >= 0x2E80 && r <= 0xA4CF && r != 0x303F) ||
		(r >= 0xAC00 && r <= 0xD7A3) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0xFE10 && r <= 0xFE19) ||
		(r >= 0xFE30 && r <= 0xFE6F) ||
		(r >= 0xFF00 && r <= 0xFF60) || // Fullwidth Forms
		(r >= 0xFFE0 && r <= 0xFFE6) ||
		(r >= 0x20000 && r <= 0x3FFFD) {
		return 2
	}
	if r > 127 {
		return 2
	}
	return 1
}

// stringWidth 精确计算字符串在终端中的总视觉列宽
func stringWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

// truncateWidth 按照终端视觉列宽截断字符串，防止因长文本自动换行破坏终端光标高度计算
func truncateWidth(s string, maxWidth int) string {
	w := 0
	var res []rune
	for _, r := range s {
		rw := runeWidth(r)
		if w+rw > maxWidth {
			break
		}
		res = append(res, r)
		w += rw
	}
	return string(res)
}

// cleanANSI 剔除字符串中的 ANSI 色彩转义字符，用于精确计算字符视觉宽度
func cleanANSI(str string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range str {
		if r == '\033' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
