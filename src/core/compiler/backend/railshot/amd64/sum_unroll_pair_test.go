//go:build linux && amd64 && wago_sumunroll

package amd64

import (
	"os"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	x86 "github.com/wago-org/wago/src/core/encoder/amd64"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func TestSumUnrollPairEmission(t *testing.T) {
	if os.Getenv("WAGO_SUM_VARIANT") != "P" {
		t.Skip("pair-tail candidate only")
	}
	selectSumUnroll(t)
	requireCompilerDiagnostics(t)
	f := &fn{a: &x86.Asm{}, stats: &CodegenStats{}, m: &wasm.Module{Memories: []wasm.MemType{{}}}, locals: []localDef{{reg: R12}, {reg: R13}, {reg: R14}}, pinnedLocalMask: regMask(0).add(R12).add(R13).add(R14), linearSumLoop: 1 | 3<<16}
	pins := f.pinned
	if !f.trySelectedLinearSumLatch(nil, 1) || f.stats.Peephole["experimental-linear-sum-pair-tail"] != 1 {
		t.Fatalf("pair tail not emitted: %v", f.stats.Peephole)
	}
	if f.a.Len() > 480 {
		t.Fatalf("pair latch exceeds bound: %d", f.a.Len())
	}
	if f.pinned != pins {
		t.Fatal("temporary pins leaked")
	}
	for _, r := range gpAlloc {
		if f.regUser[r] != nil {
			t.Fatal("temporary owner leaked")
		}
	}
}

func TestSumUnrollPairBudgetFallback(t *testing.T) {
	if os.Getenv("WAGO_SUM_VARIANT") != "P" {
		t.Skip("pair-tail candidate only")
	}
	selectSumUnroll(t)
	requireCompilerDiagnostics(t)
	sumUnrollExperiment.budget = 479
	var stats ModuleStats
	m := sumUnrollModule(t, 0)
	native := sumUnrollNative(t, m, CompileOptions{Stats: &stats})
	if stats.Funcs[0].Peephole["linear-sum-unroll4"] != 1 || stats.Funcs[0].Peephole["experimental-linear-sum"] != 0 {
		t.Fatalf("fallback not selected: %v", stats.Funcs[0].Peephole)
	}
	mem, err := rt.NewJobMemory(65536)
	if err != nil {
		t.Fatal(err)
	}
	defer mem.Close()
	for i := range mem.CurrentBytes() {
		mem.CurrentBytes()[i] = byte(i*19 + 7)
	}
	for count := uint32(0); count <= 35; count++ {
		want, trap := sumOracle(mem.CurrentBytes(), 1, count, ^uint64(0)-3, 0)
		got, err := native.call(mem, 1, count, ^uint64(0)-3, 0)
		if trap || err != nil || got != want {
			t.Fatalf("count=%d got=%v want=%v err=%v", count, got, want, err)
		}
	}
}
