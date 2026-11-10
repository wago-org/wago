package wasm

import (
	"fmt"
	"testing"
)

func BenchmarkValidatedDynamicCallFacts(b *testing.B) {
	for _, count := range []int{128, 4096} {
		m := &Module{Types: make([]RecType, count)}
		for i := range m.Types {
			m.Types[i].SubTypes = []SubType{{Comp: CompType{Kind: CompFunc, Params: []ValType{I32, I64, FuncRef}}}}
		}
		owner := moduleValidator{m: m}
		owner.freezeCompCache()
		v := funcValidator{moduleValidator: &owner}
		for _, cached := range []bool{false, true} {
			b.Run(fmt.Sprintf("groups_%d/cached_%v", count, cached), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					var facts ValidatedFuncFacts
					if cached {
						v.observeValidatedDynamicCall(&facts, uint32(count-1))
					} else {
						ft, ok := m.TypeFunc(uint32(count - 1))
						if !ok {
							b.Fatal("missing type")
						}
						for _, typ := range ft.Params {
							if typ.Kind() == ValRef {
								facts.Flags |= ValidatedFuncDynamicReferenceCall
								break
							}
						}
					}
					if facts.Flags&ValidatedFuncDynamicReferenceCall == 0 {
						b.Fatal("reference call omitted")
					}
				}
			})
		}
	}
}

func TestValidatedReferenceRequirementsIndependentOfTable(t *testing.T) {
	initValidatedFuncFlags()
	for _, kind := range []InstrKind{InstrRefAsNonNull, InstrBrOnNull, InstrBrOnNonNull} {
		var facts ValidatedFuncFacts
		facts.observe(kind)
		want := ValidatedFuncUsesReferenceTypes | ValidatedFuncUsesTypedFunctionReferences | ValidatedFuncNeedsDetailedAdmission
		if facts.Flags&want != want {
			t.Errorf("%s: flags %#x omit %#x", kind, facts.Flags, want)
		}
	}
	if validatedInstructionAdmissionComplete(InstrInvalid) || validatedInstructionAdmissionComplete(numInstrKinds) {
		t.Fatal("unknown instruction received complete admission classification")
	}
}

func TestValidatedFuncFlagsTable(t *testing.T) {
	initValidatedFuncFlags()
	for kind := InstrKind(0); kind < numInstrKinds; kind++ {
		var facts ValidatedFuncFacts
		facts.observeSlow(kind)
		if got, want := validatedFuncFlagsByKind[kind], facts.Flags; got != want {
			t.Fatalf("kind %d flags = %#x, want %#x", kind, got, want)
		}
		if got, want := validatedFuncNeedsPayloadByKind[kind], validatedInstructionNeedsPayload(kind); got != want {
			t.Fatalf("kind %d payload observation = %v, want %v", kind, got, want)
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
