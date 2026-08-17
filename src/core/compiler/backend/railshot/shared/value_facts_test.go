package shared

import (
	"testing"
	"unsafe"
)

func TestValueFactsRemainPackedAndIntersectAtMerge(t *testing.T) {
	if got := unsafe.Sizeof(ValueFacts(0)); got != 1 {
		t.Fatalf("ValueFacts size = %d, want 1", got)
	}
	left := ValueFactUpper32Zero | ValueFactBoolean | ValueFactSignExt8 | ValueFactI31
	right := ValueFactUpper32Zero | ValueFactSignExt8 | ValueFactSignExt16 | ValueFactI31
	if got, want := MergeValueFacts(left, right), ValueFactUpper32Zero|ValueFactSignExt8|ValueFactI31; got != want {
		t.Fatalf("merge = %#x, want %#x", got, want)
	}
}

func TestValueFactsForIntLoad(t *testing.T) {
	if got, want := ValueFactsForIntLoad(1, true, true), ValueFactSignExt8|ValueFactSignExt16|ValueFactSignExt32; got != want {
		t.Fatalf("i64.load8_s facts = %#x, want %#x", got, want)
	}
	if got, want := ValueFactsForIntLoad(2, true, false), ValueFactUpper32Zero|ValueFactSignExt16; got != want {
		t.Fatalf("i32.load16_s facts = %#x, want %#x", got, want)
	}
	if got, want := ValueFactsForIntLoad(4, false, false), ValueFactUpper32Zero; got != want {
		t.Fatalf("i32.load facts = %#x, want %#x", got, want)
	}
}
