package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestGCProductRequirementGate(t *testing.T) {
	saved := gcProductRequiredGateEnabled
	defer func() { gcProductRequiredGateEnabled = saved }()
	for _, on := range []bool{false, true} {
		gcProductRequiredGateEnabled = on
		for _, f := range []CoreFeatures{0, CoreFeaturesV1, CoreFeatureReferenceTypes, CoreFeatureSIMD, CoreFeatureGC, CoreFeaturesV3} {
			want := !on || f.IsEnabled(CoreFeatureGC)
			if got := gcProductAnalysisNeeded(f); got != want {
				t.Fatalf("on=%v features=%x got=%v want=%v", on, f, got, want)
			}
		}
	}
}

func TestGCInitializerProductSelection(t *testing.T) {
	for _, kind := range []wasm.InstrKind{wasm.InstrStructNew, wasm.InstrStructNewDefault, wasm.InstrArrayNew, wasm.InstrArrayNewDefault, wasm.InstrArrayNewFixed} {
		for _, location := range []string{"global", "table", "element"} {
			t.Run(fmt.Sprintf("%d/%s", kind, location), func(t *testing.T) {
				expr := wasm.Expr{Instrs: []wasm.Instruction{{Kind: kind}}}
				m := &wasm.Module{}
				switch location {
				case "global":
					m.Globals = []wasm.Global{{Init: expr}}
				case "table":
					m.Tables = []wasm.Table{{Init: &expr}}
				case "element":
					m.Elements = []wasm.Elem{{Kind: wasm.ElemKind{Exprs: []wasm.Expr{expr}}}}
				}
				wantStruct := kind == wasm.InstrStructNew || kind == wasm.InstrStructNewDefault
				if moduleUsesGenericGCStructHelpers(m) != wantStruct {
					t.Fatal("wrong struct initializer product")
				}
				product, array := stagedGCArrayOpcodeProduct(m)
				if array == wantStruct || array && product != stagedGCArrayProductGeneric {
					t.Fatal("wrong array initializer product")
				}
			})
		}
	}
	m := &wasm.Module{Globals: []wasm.Global{{Init: wasm.Expr{BodyBytes: []byte{0x41, 0x00, 0xfb, 0x00, 0x00, 0x0b}}}}}
	if !moduleUsesGenericGCStructHelpers(m) {
		t.Fatal("byte-backed struct initializer was missed")
	}
	if moduleUsesGenericGCStructHelpers(&wasm.Module{}) {
		t.Fatal("declarations gained struct helper product")
	}
}
