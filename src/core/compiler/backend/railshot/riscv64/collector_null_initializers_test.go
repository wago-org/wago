//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestAbstractCollectorNullInitializer(t *testing.T) {
	for _, heap := range []wasm.AbsHeapType{wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray, wasm.HeapNone} {
		g := wasm.Global{Type: wasm.GlobalType{Type: wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(heap), false))}, Init: wasm.Expr{BodyBytes: []byte{0xd0, byte(heap), 0x0b}}}
		if !abstractCollectorNullInitializer(g) {
			t.Fatal("collector null rejected", heap)
		}
		g.Type.Type = wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(heap), false))
		if abstractCollectorNullInitializer(g) {
			t.Fatal("nonnull collector null admitted", heap)
		}
	}
	for _, heap := range []wasm.AbsHeapType{wasm.HeapFunc, wasm.HeapExtern, wasm.HeapExn} {
		g := wasm.Global{Type: wasm.GlobalType{Type: wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(heap), false))}, Init: wasm.Expr{BodyBytes: []byte{0xd0, byte(heap), 0x0b}}}
		if abstractCollectorNullInitializer(g) {
			t.Fatal("noncollector null admitted", heap)
		}
	}
}
