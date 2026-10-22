//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRootedExactReferencesRequireAggregateDirectory(t *testing.T) {
	field := wasm.NewFieldType(wasm.StorageVal(wasm.I32), wasm.Const)
	declared := []wasm.RecType{
		{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompStruct, Fields: []wasm.FieldType{field}}}}},
		{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompArray, Array: field}}}},
		{SubTypes: []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}}}}},
	}
	directory, ok := newInternalReferenceTypeDirectory(declared)
	if !ok {
		t.Fatal("rooted directory")
	}
	types := internalReferenceTypes{decoded: declared, rootedDirectory: directory, gcRootedScalar: true}
	for index := uint32(0); index < 4; index++ {
		typ := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: index}), true))
		if got := internalFunctionReferenceWithTypes(typ, true, types); got != (index < 2) {
			t.Fatalf("exact indexed type %d accepted=%t", index, got)
		}
		if internalFunctionReferenceWithTypes(typ, true, internalReferenceTypes{decoded: declared}) {
			t.Fatalf("unrooted exact indexed type %d accepted", index)
		}
	}
	for _, heap := range []wasm.HeapType{wasm.AbsHeap(wasm.HeapAny), wasm.AbsHeap(wasm.HeapFunc), wasm.IndexedHeap(wasm.TypeIdx{Index: 0, Rec: true})} {
		if internalFunctionReferenceWithTypes(wasm.RefVal(wasm.Ref(true, heap, true)), true, types) {
			t.Fatal("unqualified exact heap accepted", heap)
		}
	}
}

func TestLegacyProfilesRejectExactReferences(t *testing.T) {
	profiles := []internalReferenceTypes{
		{gcStructBasic: true},
		{gcStructNull: true},
		{gcStructMutation: true},
		{gcStructPacked: true},
		{gcRefTestAbstract: true},
		{gcTypeSubtypingRecursiveFunctions: true},
		{gcArrayFixedOfficial: true},
	}
	for profile, types := range profiles {
		for _, heap := range []wasm.HeapType{
			wasm.AbsHeap(wasm.HeapAny), wasm.AbsHeap(wasm.HeapStruct),
			wasm.AbsHeap(wasm.HeapArray), wasm.IndexedHeap(wasm.TypeIdx{Index: 0}),
		} {
			for _, nullable := range []bool{false, true} {
				if internalFunctionReferenceWithTypes(wasm.RefVal(wasm.Ref(nullable, heap, true)), true, types) {
					t.Fatalf("profile %d accepted exact heap %v (nullable=%t)", profile, heap, nullable)
				}
			}
		}
	}
}
