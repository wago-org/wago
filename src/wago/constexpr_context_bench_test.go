package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func indexedRefNullBenchmarkModule(typeCount int) *wasm.Module {
	m := &wasm.Module{Types: make([]wasm.RecType, typeCount)}
	for i := range m.Types {
		m.Types[i].SubTypes = []wasm.SubType{{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}}}
	}
	return m
}

func TestConstExprCompileContextReusesTypeDescriptors(t *testing.T) {
	m := indexedRefNullBenchmarkModule(1024)
	ctx, err := newConstExprCompileContext(m)
	if err != nil {
		t.Fatal(err)
	}
	ref := wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 1023}), false)
	expr := append([]byte{0xd0}, wasmtest.SLEB64(1023)...)
	expr = append(expr, 0x0b)
	want := wasm.RefVal(ref)
	if allocs := testing.AllocsPerRun(1000, func() {
		if _, err := evalConstExprBytesWithContext(expr, want, ctx); err != nil {
			panic(err)
		}
	}); allocs != 0 {
		t.Fatalf("cached indexed ref.null evaluation allocs = %v, want 0", allocs)
	}
}

func BenchmarkConstExprIndexedRefNullContext(b *testing.B) {
	m := indexedRefNullBenchmarkModule(4096)
	ctx, err := newConstExprCompileContext(m)
	if err != nil {
		b.Fatal(err)
	}
	exprs := make([][]byte, 256)
	wants := make([]wasm.ValType, 256)
	for i := range exprs {
		index := uint32(i * 16)
		exprs[i] = append([]byte{0xd0}, wasmtest.SLEB64(int64(index))...)
		exprs[i] = append(exprs[i], 0x0b)
		wants[i] = wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: index}), false))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		index := i % len(exprs)
		if _, err := evalConstExprBytesWithContext(exprs[index], wants[index], ctx); err != nil {
			b.Fatal(err)
		}
	}
}
