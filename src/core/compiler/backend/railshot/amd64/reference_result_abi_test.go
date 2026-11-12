//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestReferenceResultABIReferenceFamilies(t *testing.T) {
	for _, abs := range []wasm.AbsHeapType{wasm.HeapFunc, wasm.HeapNoFunc, wasm.HeapEq, wasm.HeapAny, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray, wasm.HeapNone, wasm.HeapExtern, wasm.HeapNoExtern, wasm.HeapExn, wasm.HeapNoExn} {
		for _, nullable := range []bool{false, true} {
			ft := &wasm.CompType{Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.RefVal(wasm.Ref(nullable, wasm.AbsHeap(abs), false))}}
			want := abs == wasm.HeapFunc || abs == wasm.HeapNoFunc
			if got := sigFitsReferenceResultRegABI(ft, nil); got != want {
				t.Errorf("heap=%x nullable=%t got%t want%t", abs, nullable, got, want)
			}
		}
	}
	for _, recursive := range []bool{false, true} {
		for _, kind := range []wasm.CompTypeKind{wasm.CompFunc, wasm.CompStruct, wasm.CompArray} {
			m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: wasm.CompStruct}}}}, {SubTypes: []wasm.SubType{{Comp: wasm.CompType{Kind: kind}}, {Comp: wasm.CompType{Kind: wasm.CompFunc}}}}}}
			index := uint32(1)
			if recursive {
				index = 0
			}
			ft := &m.Types[1].SubTypes[1].Comp
			ft.Params = []wasm.ValType{wasm.I32}
			ft.Results = []wasm.ValType{wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: index, Rec: recursive}), false))}
			if got := sigFitsReferenceResultRegABI(ft, m); got != (kind == wasm.CompFunc) {
				t.Errorf("recursive=%t kind=%v got%t", recursive, kind, got)
			}
			ft.Results = []wasm.ValType{wasm.RefVal(wasm.Ref(true, wasm.DefinedHeap(&wasm.DefType{GroupIndex: 1, Index: 0, Rec: m.Types[1]}), false))}
			if got := sigFitsReferenceResultRegABI(ft, m); got != (kind == wasm.CompFunc) {
				t.Errorf("defined kind=%v got%t", kind, got)
			}
		}
	}
}
