//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"math/bits"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func popcountLoopBody(variant int) []byte {
	constant := func(v int32) []byte { return append([]byte{0x41}, wasmtest.SLEB32(v)...) }
	update := func(local byte, delta int32, subtract, constantFirst bool) []byte {
		b := []byte{0x20, local}
		if constantFirst {
			b = append(constant(delta), b...)
		} else {
			b = append(b, constant(delta)...)
		}
		if subtract {
			b = append(b, 0x6b)
		} else {
			b = append(b, 0x6a)
		}
		return append(b, 0x21, local)
	}
	a := update(1, 3, false, variant&1 != 0)
	b := update(2, 2, true, false)
	if variant&2 != 0 {
		a, b = b, a
	}
	body := []byte{0}
	body = append(body, 0x03, 0x40)
	body = append(body, a...)
	body = append(body, b...)
	if variant&4 == 0 {
		body = append(body, 0x20, 0)
		body = append(body, constant(-1)...)
		body = append(body, 0x6a, 0x20, 0, 0x71)
	} else {
		body = append(body, 0x20, 0, 0x20, 0)
		body = append(body, constant(1)...)
		body = append(body, 0x6b, 0x71)
	}
	body = append(body, 0x22, 0, 0x0d, 0, 0x0b)
	// Return b:a+x so every summarized local remains observable.
	body = append(body, 0x20, 2, 0xad, 0x42, 32, 0x86, 0x20, 1, 0x20, 0, 0x6a, 0xad, 0x84, 0x0b)
	return body
}

func popcountLoopModule(t testing.TB, variant int) *wasm.Module {
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I64}, popcountLoopBody(variant))
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPopcountLoopSummarySemantics(t *testing.T) {
	old := popcountLoopSummaryEnabled
	t.Cleanup(func() { popcountLoopSummaryEnabled = old })
	for variant := 0; variant < 8; variant++ {
		m := popcountLoopModule(t, variant)
		for _, on := range []bool{false, true} {
			popcountLoopSummaryEnabled = on
			for _, compact := range []bool{false, true} {
				for _, x := range []uint32{0, 1, 2, 3, 0x80000000, 0x55555555, ^uint32(0)} {
					n := bits.OnesCount32(x)
					if n == 0 {
						n = 1
					}
					a0, b0 := uint32(0x80000007), uint32(0x7ffffff9)
					want := uint64(b0-uint32(2*n))<<32 | uint64(a0+uint32(3*n))
					got, err := runArm64WrapperWithOptions(t, m, CompileOptions{CompactNative: compact}, uint64(x), uint64(a0), uint64(b0))
					if err != nil || got != want {
						t.Fatalf("variant%d on%v compact%v x%x got%x want%x err%v", variant, on, compact, x, got, want, err)
					}
				}
			}
		}
	}
}

func TestPopcountLoopSummaryAdmission(t *testing.T) {
	requireCompilerDiagnostics(t)
	if profileEnabled {
		t.Skip("ordinary emitter qualification")
	}
	old := popcountLoopSummaryEnabled
	t.Cleanup(func() { popcountLoopSummaryEnabled = old })
	for _, on := range []bool{false, true} {
		popcountLoopSummaryEnabled = on
		var stats ModuleStats
		cm, err := CompileModuleWith(popcountLoopModule(t, 7), CompileOptions{Stats: &stats})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		if hit := stats.Funcs[0].Peephole["popcount-loop-summary"]; (hit != 0) != on {
			t.Fatalf("on%v hit%d all%v", on, hit, stats.Funcs[0].Peephole)
		}
	}
}

func TestPopcountLoopSummaryProbeBudgetAndRejections(t *testing.T) {
	f := &fn{nLocals: 3, localType: []machineType{mtI32, mtI32, mtI32}}
	body := popcountLoopBody(0)[3:]
	if allocs := testing.AllocsPerRun(100, func() {
		f.affineLoopWork = 0
		r := wasm.ReaderFrom(body)
		var p popcountLoopPlan
		if !f.inspectPopcountLoop(&r, &p) || p.n != 3 || p.pop < 0 || r.Offset() != 0 {
			panic("invalid popcount probe")
		}
	}); allocs != 0 {
		t.Fatalf("probe allocations%g", allocs)
	}
	for _, mutate := range []func([]byte) []byte{
		func(b []byte) []byte { return append([]byte{0x20, 1, 0x20, 2, 0x6a, 0x21, 1}, b...) },
		func(b []byte) []byte { return append([]byte{0x20, 0, 0x41, 2, 0x6a, 0x21, 0}, b...) },
		func(b []byte) []byte { return append([]byte{0x28, 2, 0}, b...) },
		func(b []byte) []byte { return append(bytes.Repeat([]byte{0x01}, 65), b...) },
	} {
		f.affineLoopWork = 0
		r := wasm.ReaderFrom(mutate(append([]byte{}, body...)))
		var p popcountLoopPlan
		if f.inspectPopcountLoop(&r, &p) || r.Offset() != 0 {
			t.Fatal("admitted unsupported popcount loop or changed reader")
		}
	}
	f.affineLoopWork = affineLoopWorkLimit - 1
	r := wasm.ReaderFrom(body)
	var p popcountLoopPlan
	if f.inspectPopcountLoop(&r, &p) || f.affineLoopWork != affineLoopWorkLimit || r.Offset() != 0 {
		t.Fatal("probe work cap or reader contract")
	}
}
