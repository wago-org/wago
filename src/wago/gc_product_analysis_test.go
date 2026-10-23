package wago

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
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
func TestGCProductGatePreservesArtifacts(t *testing.T) {
	saved := gcProductRequiredGateEnabled
	defer func() { gcProductRequiredGateEnabled = saved }()
	function := func(body []byte) []byte {
		return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
	}
	for _, tc := range []struct {
		name string
		data []byte
		gc   bool
	}{
		{"plain-with-prefix-in-immediate", function([]byte{0x41, 0xfb, 0x01, 0x1a, 0x41, 42, 0x0b}), false},
		{"abstract-i31", function([]byte{0x41, 42, 0xfb, 0x1c, 0xfb, 0x14, 0x6c, 0x0b}), true},
		{"extern-conversion", function([]byte{0xd0, 0x6f, 0xfb, 0x1a, 0xfb, 0x1b, 0xd1, 0x0b}), true},
		{"struct", stagedGCStructGetOnlyBytes(t), true},
		{"array", stagedGCArrayNumericLocalBytes(t), true},
		{"array-declaration", wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}))), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := wasm.DecodeModule(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			if got := moduleRequiredFeatures(m).IsEnabled(CoreFeatureGC); got != tc.gc {
				t.Fatalf("GC requirement=%v want=%v", got, tc.gc)
			}
			features := CoreFeaturesV1
			if tc.gc {
				requireCompleteCore3Backend(t)
				features = CoreFeaturesV3
			}
			var want []byte
			for _, on := range []bool{false, true} {
				gcProductRequiredGateEnabled = on
				c, err := NewRuntimeConfig().WithCoreFeatures(features).WithBoundsChecks(BoundsChecksExplicit).Compile(tc.data)
				if err != nil {
					t.Fatal(err)
				}
				if retained := len(c.GCTypeDescs) != 0; retained != tc.gc {
					t.Fatalf("collector descriptors retained=%v want=%v", retained, tc.gc)
				}
				b, err := marshalCompiled(c)
				c.Close()
				if err != nil {
					t.Fatal(err)
				}
				if !on {
					want = b
				} else if !bytes.Equal(want, b) {
					t.Fatal("gate changed persisted code or metadata")
				}
			}
		})
	}
}

func TestCollectorFreeMetadataRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  wasm.ValType
		abi  ValType
		arg  uint64
	}{
		{"i32", wasm.I32, ValI32, I32(42)},
		{"f64", wasm.F64, ValF64, F64(3.5)},
		{"funcref", wasm.FuncRef, ValFuncRef, 0},
		{"externref", wasm.ExternRef, ValExternRef, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{tc.typ}, []wasm.ValType{tc.typ}))),
				wasmtest.Section(3, wasmtest.Vec([]byte{0})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("id", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}))),
			)
			compiled, err := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).Compile(data)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			if len(compiled.GCTypeDescs) != 0 || len(compiled.Types) != 1 {
				t.Fatalf("type tables: collector=%d function=%d", len(compiled.GCTypeDescs), len(compiled.Types))
			}
			cached := publicArtifactRoundTrip(t, compiled)
			defer cached.Close()
			// Older artifacts carried redundant function sentinels. They remain
			// valid and must execute identically to the smaller representation.
			legacy := mutableCompiledFixture(compiled)
			defer legacy.Close()
			legacy.GCTypeDescs = []gc.TypeDesc{{ID: 0, Kind: gc.KindFunc, Final: true}}
			cachedLegacy := publicArtifactRoundTrip(t, legacy)
			defer cachedLegacy.Close()
			for _, c := range []*Compiled{compiled, cached, cachedLegacy} {
				params, results, err := c.Signature("id")
				if err != nil || len(params) != 1 || len(results) != 1 || params[0] != tc.abi || results[0] != tc.abi {
					t.Fatalf("signature=%v -> %v, %v", params, results, err)
				}
				in, err := Instantiate(c, InstantiateOptions{})
				if err != nil {
					t.Fatal(err)
				}
				got, err := in.Invoke("id", tc.arg)
				in.Close()
				if err != nil || len(got) != 1 || got[0] != tc.arg {
					t.Fatalf("identity=%v, %v; want %d", got, err, tc.arg)
				}
			}
		})
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
