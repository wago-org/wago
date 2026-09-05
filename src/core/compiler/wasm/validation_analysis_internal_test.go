package wasm

import "testing"

func TestValidatedFuncFlagsTable(t *testing.T) {
	initValidatedFuncFlags()
	for kind := InstrKind(0); kind < numInstrKinds; kind++ {
		var facts ValidatedFuncFacts
		facts.observeSlow(kind)
		if got, want := validatedFuncFlagsByKind[kind], facts.Flags; got != want {
			t.Fatalf("kind %d flags = %#x, want %#x", kind, got, want)
		}
	}
}

func TestSegmentStateCount(t *testing.T) {
	for _, tc := range []struct {
		index uint32
		want  uint32
	}{
		{0, 1},
		{254, 255},
		{255, 256},
		{^uint32(0), ^uint32(0)},
	} {
		if got := segmentStateCount(tc.index); got != tc.want {
			t.Errorf("segmentStateCount(%d) = %d, want %d", tc.index, got, tc.want)
		}
	}
}

func TestRecordValidatedAnalysisSegmentCounts(t *testing.T) {
	var v funcValidator
	var facts ValidatedFuncFacts
	var counts validationSegmentCounts
	for _, in := range []Instruction{
		{Kind: InstrDataDrop, Index: 299},
		{Kind: InstrMemoryInit, Index: 7},
		{Kind: InstrElemDrop, Index: 399},
		{Kind: InstrTableInit, Index: 9},
	} {
		v.observeValidatedInstruction(&facts, &in, &counts)
	}
	if counts.data != 300 || counts.elem != 400 {
		t.Fatalf("segment counts = data:%d element:%d, want 300/400", counts.data, counts.elem)
	}
}
