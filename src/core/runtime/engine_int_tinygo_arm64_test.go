//go:build tinygo && (linux || darwin) && arm64

package runtime

import "testing"

func TestTinyGoARM64EntrySharesCodeAcrossIndependentStacks(t *testing.T) {
	first, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	entries := tinygoARM64Entries.Load()
	second, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if entries == nil || entries != tinygoARM64Entries.Load() {
		t.Fatal("engines did not share the same native entry code")
	}
	if first.stackTop != first.StackTop() || second.stackTop != second.StackTop() || first.stackTop == second.stackTop {
		t.Fatal("engines did not retain independent physical stacks")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if first.stackTop != 0 || tinygoARM64Entries.Load() != entries || second.stackTop == 0 {
		t.Fatal("closing one engine changed shared code or another engine's stack")
	}
}
