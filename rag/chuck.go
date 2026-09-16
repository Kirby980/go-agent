package rag

import "strings"

type RecursiveChunker struct {
	ChunkSize  int      // 每块目标最大长度（按 rune 计，对中文友好）
	Overlap    int      // 相邻块重叠长度
	Separators []string // 分隔符，从粗到细，如 ["\n\n", "\n", "。", "！", "？", " "]
}

func NewRecursiveChunker(chunkSize, overlap int) *RecursiveChunker {
	return &RecursiveChunker{
		ChunkSize:  chunkSize,
		Overlap:    overlap,
		Separators: []string{"\n\n", "\n", "。", "！", "？", "; ", " "},
	}
}

func (c *RecursiveChunker) Split(text string) []string {
	atoms := c.recurse(text, 0)
	return c.addOverlap(atoms)
}

func (c *RecursiveChunker) recurse(text string, sepIdx int) []string {
	if len([]rune(text)) <= c.ChunkSize {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []string{text}
	}
	if sepIdx >= len(c.Separators) {
		return hardSplit([]rune(text), c.ChunkSize) // 没有更细的分隔符了，硬切
	}

	sep := c.Separators[sepIdx]
	parts := strings.Split(text, sep)

	// 把仍然过大的 part 用更细的分隔符进一步切碎，得到一串“原子片段”
	var atoms []string
	for _, p := range parts {
		if len([]rune(p)) > c.ChunkSize {
			atoms = append(atoms, c.recurse(p, sepIdx+1)...)
		} else if strings.TrimSpace(p) != "" {
			atoms = append(atoms, p)
		}
	}
	// 贪心合并相邻原子，尽量填满每一块
	return mergeAtoms(atoms, sep, c.ChunkSize)
}

// hardSplit 在没有任何自然边界时按定长硬切（最后的兜底）。
func hardSplit(runes []rune, size int) []string {
	var out []string
	for i := 0; i < len(runes); i += size {
		end := i + size
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	return out
}

// mergeAtoms 用分隔符把原子片段贪心拼成 ≤ chunkSize 的块。
func mergeAtoms(atoms []string, sep string, chunkSize int) []string {
	var out []string
	var cur strings.Builder
	curLen := 0
	flush := func() {
		if curLen > 0 {
			out = append(out, cur.String())
			cur.Reset()
			curLen = 0
		}
	}
	sepLen := len([]rune(sep))
	for _, a := range atoms {
		al := len([]rune(a))
		add := al
		if curLen > 0 {
			add += sepLen
		}
		if curLen > 0 && curLen+add > chunkSize {
			flush()
			add = al // 新块开头不加分隔符
		}
		if curLen > 0 {
			cur.WriteString(sep)
		}
		cur.WriteString(a)
		curLen += add
	}
	flush()
	return out
}

func (c *RecursiveChunker) addOverlap(chunks []string) []string {
	if c.Overlap <= 0 || len(chunks) <= 1 {
		return chunks
	}
	out := make([]string, len(chunks))
	out[0] = chunks[0]
	for i := 1; i < len(chunks); i++ {
		prev := []rune(chunks[i-1])
		tail := prev
		if len(prev) > c.Overlap {
			tail = prev[len(prev)-c.Overlap:]
		}
		out[i] = string(tail) + chunks[i]
	}
	return out
}
