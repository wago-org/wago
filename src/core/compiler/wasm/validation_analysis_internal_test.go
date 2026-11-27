package wasm

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/internal/buildfeatures"
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

func TestValidatedDirectInstructionFacts(t *testing.T) {
	for _, tc := range []struct {
		name string
		code []byte
		want ValidatedFuncFlags
	}{
		{"scalar", []byte{0x41, 0, 0x41, 1, 0x6a, 0x1a}, 0},
		{"sign extension", []byte{0x41, 0, 0xc0, 0x1a}, ValidatedFuncUsesSignExtension},
		{"global", []byte{0x23, 0, 0x1a}, ValidatedFuncTouchesGlobal},
		{"load", []byte{0x41, 0, 0x28, 2, 0, 0x1a}, ValidatedFuncTouchesMemory | ValidatedFuncUsesNonAtomicMemory},
		{"branch", []byte{0x41, 0, 0x0d, 0}, ValidatedFuncHasControl},
		{"call", []byte{0x10, 0}, ValidatedFuncHasDirectCall | ValidatedFuncMayCollect},
		{"tail call", []byte{0x12, 0}, ValidatedFuncHasDirectCall | ValidatedFuncMayCollect |
			ValidatedFuncHasTailCall | ValidatedFuncHasControl |
			ValidatedFuncNeedsDetailedAdmission | ValidatedFuncNeedsDetailedRequirements},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.want == ValidatedFuncUsesSignExtension && signExtensionBuilt == 0 {
				t.Skip("requires sign-extension ops")
			}
			if tc.want&ValidatedFuncHasTailCall != 0 && buildfeatures.TailCall == 0 {
				t.Skip("requires tail calls")
			}
			m := modWithFunc(nil, nil)
			m.Code[0].BodyBytes = append(tc.code, 0x0b)
			m.Memories = []MemType{{Limits: Limits{Min: 1}}}
			m.Globals = []Global{{Type: GlobalType{Type: I32}, Init: Expr{Instrs: []Instruction{{Kind: InstrI32Const}}}}}
			var analysis ValidatedModuleAnalysis
			if err := ValidateModuleWithAnalysis(m, ValidationFeatures{}, 1, ValidationLimits{}, &analysis); err != nil {
				t.Fatal(err)
			}
			if !analysis.ValidFor(m) {
				t.Fatal("validation did not produce analysis")
			}
			if got := analysis.Func(0).Flags; got != tc.want {
				t.Fatalf("function flags = %#x, want %#x", got, tc.want)
			}
		})
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
