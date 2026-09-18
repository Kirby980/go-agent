package rag

import (
	"reflect"
	"testing"
)

func TestRRF(t *testing.T) {
	tests := []struct {
		name     string
		rankings [][]string
		k        int
		expected []string
	}{
		{
			name:     "empty rankings",
			rankings: [][]string{},
			k:        60,
			expected: []string{},
		},
		{
			name: "single list ranking order preserved",
			rankings: [][]string{
				{"docA", "docB", "docC"},
			},
			k:        60,
			expected: []string{"docA", "docB", "docC"},
		},
		{
			name: "two lists consensus moves item to top",
			// docB is ranked 2nd in both (rank 1), docA is 1st in list1 (rank 0) but absent in list2
			// score(docB) = 1/(60+1+1) + 1/(60+1+1) = 2/62 ≈ 0.032258
			// score(docA) = 1/(60+0+1) = 1/61 ≈ 0.016393
			// score(docC) = 1/(60+0+1) = 1/61 ≈ 0.016393
			rankings: [][]string{
				{"docA", "docB"},
				{"docC", "docB"},
			},
			k:        60,
			expected: []string{"docB", "docA", "docC"},
		},
		{
			name: "different k factor effect",
			rankings: [][]string{
				{"doc1", "doc2"},
				{"doc2", "doc1"},
			},
			k: 10,
			// both have identical combined score: 1/(10+1) + 1/(10+2) = 1/11 + 1/12
			// ordering between doc1 and doc2 is tied, but both must exist in result
			expected: []string{"doc1", "doc2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RRF(tt.rankings, tt.k)
			if tt.name == "two lists consensus moves item to top" {
				if len(got) != 3 || got[0] != "docB" {
					t.Fatalf("expected docB to be top ranked, got %v", got)
				}
				return
			}
			if tt.name == "different k factor effect" {
				if len(got) != 2 {
					t.Fatalf("expected 2 items, got %v", got)
				}
				return
			}
			if !reflect.DeepEqual(got, tt.expected) && !(len(got) == 0 && len(tt.expected) == 0) {
				t.Errorf("RRF() = %v, want %v", got, tt.expected)
			}
		})
	}
}
