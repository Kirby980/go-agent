package rag

import (
	"strings"
	"testing"
)

func TestRecursiveChunker_Split(t *testing.T) {
	tests := []struct {
		name      string
		chunkSize int
		overlap   int
		input     string
		validate  func(t *testing.T, chunks []string)
	}{
		{
			name:      "empty input",
			chunkSize: 100,
			overlap:   10,
			input:     "   \n\n  \t ",
			validate: func(t *testing.T, chunks []string) {
				if len(chunks) != 0 {
					t.Fatalf("expected 0 chunks, got %d", len(chunks))
				}
			},
		},
		{
			name:      "short text within chunk size",
			chunkSize: 50,
			overlap:   5,
			input:     "这是一个简短的句子，不会被切分。",
			validate: func(t *testing.T, chunks []string) {
				if len(chunks) != 1 {
					t.Fatalf("expected 1 chunk, got %d", len(chunks))
				}
				if chunks[0] != "这是一个简短的句子，不会被切分。" {
					t.Errorf("unexpected content: %s", chunks[0])
				}
			},
		},
		{
			name:      "split by natural paragraph delimiters",
			chunkSize: 15,
			overlap:   0,
			input:     "第一段内容比较长。\n\n第二段内容也很丰富。\n\n第三段补充说明。",
			validate: func(t *testing.T, chunks []string) {
				if len(chunks) < 3 {
					t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
				}
				for _, ch := range chunks {
					if len([]rune(ch)) > 15 {
						t.Errorf("chunk exceeds max size 15: %s", ch)
					}
				}
			},
		},
		{
			name:      "hard split for super long string without any separators",
			chunkSize: 10,
			overlap:   0,
			// 35 characters with no punctuation or whitespace
			input: "abcdefghijklmnopqrstuvwxyz012345678",
			validate: func(t *testing.T, chunks []string) {
				if len(chunks) != 4 {
					t.Fatalf("expected 4 chunks, got %d", len(chunks))
				}
				for i, ch := range chunks {
					if len([]rune(ch)) > 10 {
						t.Errorf("chunk %d exceeded chunkSize: length %d, text: %s", i, len([]rune(ch)), ch)
					}
				}
				recombined := strings.Join(chunks, "")
				if recombined != "abcdefghijklmnopqrstuvwxyz012345678" {
					t.Errorf("expected concatenated chunks to equal original string, got %s", recombined)
				}
			},
		},
		{
			name:      "hard split chinese string without separators",
			chunkSize: 5,
			overlap:   0,
			input:     "超长连续中文没有任何标点符号硬切测试",
			validate: func(t *testing.T, chunks []string) {
				if len(chunks) != 4 {
					t.Fatalf("expected 4 chunks of size <= 5, got %d: %v", len(chunks), chunks)
				}
				for _, ch := range chunks {
					if len([]rune(ch)) > 5 {
						t.Errorf("chunk exceeds max size 5: %s", ch)
					}
				}
			},
		},
		{
			name:      "with overlap",
			chunkSize: 10,
			overlap:   3,
			input:     "第一句话。第二句话。第三句话。",
			validate: func(t *testing.T, chunks []string) {
				if len(chunks) > 1 {
					// 验证第二块包含了第一块末尾的字符
					firstTail := string([]rune(chunks[0])[len([]rune(chunks[0]))-3:])
					if !strings.HasPrefix(chunks[1], firstTail) {
						t.Errorf("chunk 1 should start with tail of chunk 0 (%s), got: %s", firstTail, chunks[1])
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunker := NewRecursiveChunker(tt.chunkSize, tt.overlap)
			chunks := chunker.Split(tt.input)
			tt.validate(t, chunks)
		})
	}
}
