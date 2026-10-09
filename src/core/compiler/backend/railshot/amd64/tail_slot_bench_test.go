//go:build amd64 && (linux || darwin || windows)

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func tailSlotBenchModule(tb testing.TB, n int) *wasm.Module {
	params := make([]wasm.ValType, 8)
	for i := range params {
		params[i] = wasm.I64
	}
	body := []byte{0}
	for i := 0; i < n; i++ {
		body = append(body, 0x41, 1)
	}
	for i := 0; i < 8; i++ {
		body = append(body, 0x42, byte(i+1))
	}
	body = append(body, 0x12, 0, 0x0b)
	m, err := wasm.DecodeModule(benchModuleBytes([]benchFuncDef{{params: params, results: []wasm.ValType{wasm.I64}, body: []byte{0, 0x20, 7, 0x0b}}, {results: []wasm.ValType{wasm.I64}, body: body}}, false))
	if err != nil {
		tb.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		tb.Fatal(err)
	}
	return m
}

var tailSlotCompileOptions = CompileOptions{Workers: 1, Optimizations: map[string]bool{"reg-abi": false, "inline": false}}

func BenchmarkCompileTailArgumentPrefix(b *testing.B) {
	m := tailSlotBenchModule(b, 8192)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cm, err := CompileModuleWith(m, tailSlotCompileOptions)
		if err != nil {
			b.Fatal(err)
		}
		benchCompiledSink = cm
		// Close each owned native image outside compilation timing and allocation accounting.
		b.StopTimer()
		if cm.CodeImage != nil {
			if err := cm.CodeImage.Close(); err != nil {
				b.Fatal(err)
			}
		}
		b.StartTimer()
	}
}

func TestTailArgumentSlotsMixedPrefix(t *testing.T) {
	m := tailSlotBenchModule(t, 8)
	m.Code[1].BodyBytes = append(benchV128Const(1, 2), m.Code[1].BodyBytes...)
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	cm, err := CompileModuleWith(m, tailSlotCompileOptions)
	if err != nil {
		t.Fatal(err)
	}
	cm.Entry[0] = cm.Entry[1]
	if got := runCompiledAmd64u(t, cm); got != 8 {
		t.Fatalf("result=%d", got)
	}
	if cm.CodeImage != nil {
		if err := cm.CodeImage.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

var tailArgumentSlotSink int

func BenchmarkTailArgumentSlotPlanning(b *testing.B) {
	nodes := make([]elem, 8192+16)
	roots := make([]*elem, len(nodes))
	for i := range nodes {
		nodes[i].st = storage{kind: stConst, typ: mtI64}
		roots[i] = &nodes[i]
	}
	params := make([]wasm.ValType, 16)
	for i := range params {
		params[i] = wasm.I64
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		srcSlot := tailArgumentStartSlot(roots, len(params))
		sum := 0
		for _, param := range params {
			sum += srcSlot
			srcSlot += mtOf(param).stackSlots()
		}
		tailArgumentSlotSink = sum
	}
}
