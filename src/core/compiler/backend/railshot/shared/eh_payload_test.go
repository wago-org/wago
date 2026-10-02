package shared

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/nativeabi"
)

// Exception payload words are classified like frame locals. A DefType-backed
// function reference was classified as a collector reference, so the root
// maps would have scanned a funcref descriptor as a gc.Ref; extern and exn
// references are not collector-owned and stay unrooted. Collector references
// use the GC lanes so a catch_all_ref slot is scanned exactly for any tag.
func TestEHPayloadRootKindAndLane(t *testing.T) {
	def := func(kind wasm.CompTypeKind) wasm.ValType {
		rec := wasm.RecType{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: kind}}}}
		return wasm.RefVal(wasm.Ref(true, wasm.DefinedHeap(&wasm.DefType{Rec: rec}), false))
	}
	abs := func(heap wasm.AbsHeapType) wasm.ValType { return wasm.RefVal(wasm.AbsRef(heap)) }
	m := &wasm.Module{}
	for _, tc := range []struct {
		name string
		typ  wasm.ValType
		kind nativeabi.RootKind
		gc   bool
	}{
		{"i64", wasm.I64, 0, false},
		{"funcref", wasm.FuncRef, nativeabi.RootFuncRef, false},
		{"deftype func", def(wasm.CompFunc), nativeabi.RootFuncRef, false},
		{"deftype struct", def(wasm.CompStruct), nativeabi.RootGCRef, true},
		{"anyref", wasm.AnyRef, nativeabi.RootGCRef, true},
		{"i31ref", abs(wasm.HeapI31), nativeabi.RootGCRef, true},
		{"externref", wasm.ExternRef, 0, false},
		{"exnref", abs(wasm.HeapExn), 0, false},
	} {
		kind, rooted := EHPayloadRootKind(m, tc.typ)
		if kind != tc.kind || rooted != (tc.kind != 0) {
			t.Errorf("%s root kind = %d/%v, want %d", tc.name, kind, rooted, tc.kind)
		}
		want := 3
		if tc.gc {
			want = EHGCLaneBase + 3
		}
		if lane := EHPayloadLane(m, tc.typ, 3); lane != want {
			t.Errorf("%s lane = %d, want %d", tc.name, lane, want)
		}
	}
}
