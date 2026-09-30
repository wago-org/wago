package wasm

import (
	"fmt"
	"testing"
)

func TestBranchHintsForFuncLookup(t *testing.T) {
	m := &Module{BranchHints: []FuncBranchHints{
		{FuncIndex: 2, Hints: []BranchHint{{}}},
		{FuncIndex: 7, Hints: []BranchHint{{}, {}}},
		{FuncIndex: 100, Hints: []BranchHint{{}, {}, {}}},
	}}
	for _, tc := range []struct {
		index uint32
		count int
	}{{0, 0}, {1, 0}, {2, 1}, {3, 0}, {6, 0}, {7, 2}, {8, 0}, {99, 0}, {100, 3}, {101, 0}, {^uint32(0), 0}} {
		got := m.BranchHintsForFunc(tc.index)
		if len(got) != tc.count {
			t.Fatalf("index %d: got %d hints, want %d", tc.index, len(got), tc.count)
		}
	}
	if (&Module{}).BranchHintsForFunc(0) != nil {
		t.Fatal("empty module has hints")
	}
	got := m.BranchHintsForFunc(7)
	if &got[0] != &m.BranchHints[1].Hints[0] {
		t.Fatal("lookup must alias immutable hint storage")
	}
}

var branchHintLookupSink int

func BenchmarkBranchHintsForFunc(b *testing.B) {
	for _, count := range []int{0, 1, 8, 512, 4096} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			m := &Module{BranchHints: make([]FuncBranchHints, count)}
			hints := []BranchHint{{}}
			for i := range m.BranchHints {
				m.BranchHints[i] = FuncBranchHints{FuncIndex: uint32(i * 2), Hints: hints}
			}
			queries := max(count*2, 1)
			b.ReportAllocs()
			b.ResetTimer()
			sum := 0
			for i := 0; i < b.N; i++ {
				for q := 0; q < queries; q++ {
					sum += len(m.BranchHintsForFunc(uint32(q)))
				}
			}
			branchHintLookupSink = sum
		})
	}
}

func TestBranchHintsForFuncLargeTables(t *testing.T) {
	for _, count := range []int{8, 9, 64, 257} {
		m := &Module{BranchHints: make([]FuncBranchHints, count)}
		hints := make([]BranchHint, count)
		for i := range m.BranchHints {
			m.BranchHints[i] = FuncBranchHints{FuncIndex: uint32(i*3 + 1), Hints: hints[i : i+1]}
		}
		for query := uint32(0); query < uint32(count*3+2); query++ {
			got := m.BranchHintsForFunc(query)
			if query%3 == 1 && query/3 < uint32(count) {
				if len(got) != 1 || &got[0] != &hints[query/3] {
					t.Fatalf("count=%d query=%d: wrong hint slice", count, query)
				}
			} else if got != nil {
				t.Fatalf("count=%d query=%d: unexpected hints", count, query)
			}
		}
		if m.BranchHintsForFunc(^uint32(0)) != nil {
			t.Fatal("maximum absent index returned hints")
		}
	}
}
