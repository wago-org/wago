//go:build linux && amd64

package amd64

import (
	"bytes"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestModuleGlobalMembershipUsesBorrowedBoundedPins(t *testing.T) {
	const nGlobals = 4096
	f := fn{
		m:           &wasm.Module{Globals: make([]wasm.Global, nGlobals)},
		globalReg:   make([]Reg, nGlobals),
		globalDirty: make([]bool, nGlobals),
	}
	pins := []moduleGlobalPin{{global: 123, reg: moduleGlobalRegs[0]}}
	if allocs := testing.AllocsPerRun(100, func() {
		f.installModuleGlobals(pins)
	}); allocs != 0 {
		t.Fatalf("module-global membership allocations = %v, want 0", allocs)
	}
	if !f.isModuleGlobal(123) || f.isModuleGlobal(124) {
		t.Fatalf("module-global membership mismatch")
	}
}

func TestCompileModuleHintLocalCountAllocationAndCodeIdentity(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "..", "..", "bench", "corpus")
	tests := []struct {
		name      string
		module    *wasm.Module
		maxAllocs float64
	}{
		{name: "tiny", module: readParallelTestModule(t, filepath.Join(root, "tiny.wasm")), maxAllocs: 32},
		{name: "many_funcs", module: readParallelTestModule(t, filepath.Join(root, "many_funcs.wasm")), maxAllocs: 360},
		{name: "blake-as", module: readParallelTestModule(t, filepath.Join(root, "blake-as.wasm")), maxAllocs: 190},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			baseline, err := CompileModuleWith(tc.module, CompileOptions{Workers: 1})
			if err != nil {
				t.Fatalf("CompileModuleWith baseline: %v", err)
			}
			allocs := testing.AllocsPerRun(20, func() {
				got, err := CompileModuleWith(tc.module, CompileOptions{Workers: 1})
				if err != nil {
					t.Fatalf("CompileModuleWith: %v", err)
				}
				if !bytes.Equal(got.Code, baseline.Code) {
					t.Fatal("native code changed across identical serial compiles")
				}
				benchCompiledSink = got
			})
			t.Logf("CompileModuleWith allocations = %.0f", allocs)
			if allocs > tc.maxAllocs {
				t.Fatalf("CompileModuleWith allocations = %.0f, budget = %.0f", allocs, tc.maxAllocs)
			}
		})
	}
}

func TestCompileSmallScalarAllocationBudget(t *testing.T) {
	m := benchSmallScalarModule(t)
	allocs := testing.AllocsPerRun(50, func() {
		cm, err := CompileModule(m)
		if err != nil {
			t.Fatalf("CompileModule: %v", err)
		}
		benchCompiledSink = cm
	})
	// Intentionally conservative: the compile benchmark is currently ~34
	// allocs/op on linux/amd64 Go 1.24, but this test is meant to catch
	// obvious allocation cliffs without flapping across Go versions or CI hosts.
	const budget = 80.0
	if allocs > budget {
		t.Fatalf("allocations = %.1f, budget = %.1f", allocs, budget)
	}
}

func TestCompileSIMDHeavyAllocationBudget(t *testing.T) {
	m := benchSIMDHeavyModule(t)
	allocs := testing.AllocsPerRun(50, func() {
		cm, err := CompileModule(m)
		if err != nil {
			t.Fatalf("CompileModule: %v", err)
		}
		benchCompiledSink = cm
	})
	// Intentionally conservative: the compile benchmark is currently ~24
	// allocs/op on linux/amd64 Go 1.24, but this test is meant to catch
	// obvious allocation cliffs without asserting a tiny exact count.
	const budget = 80.0
	if allocs > budget {
		t.Fatalf("allocations = %.1f, budget = %.1f", allocs, budget)
	}
}

func TestCompileParallelAllocationBudget(t *testing.T) {
	m := readParallelTestModule(t, filepath.Join("..", "..", "..", "..", "..", "..", "bench", "corpus", "json-as.wasm"))
	oldProcs := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(oldProcs)

	allocs := func(workers int) float64 {
		return testing.AllocsPerRun(10, func() {
			cm, err := CompileModuleWith(m, CompileOptions{Workers: workers})
			if err != nil {
				t.Fatalf("CompileModuleWith workers=%d: %v", workers, err)
			}
			benchCompiledSink = cm
		})
	}
	serial := allocs(1)
	parallel := allocs(4)
	t.Logf("json-as backend allocations: p1=%.1f p4=%.1f", serial, parallel)

	// Measured locally on linux/amd64 Go 1.24.4: p1=1,286 and p4=1,286
	// allocations/op. The p4 path intentionally allocates more bytes (the worker
	// report measured about 363 KiB/op at p1 and 702 KiB/op at p4), even though the
	// allocation-event count is currently equal. These ceilings leave broad Go/CI
	// margin while catching per-instruction allocation or recreating all function
	// metadata independently in every worker.
	const (
		serialBudget   = 5000.0
		parallelBudget = 8000.0
	)
	if serial > serialBudget {
		t.Fatalf("serial allocations = %.1f, budget = %.1f", serial, serialBudget)
	}
	if parallel > parallelBudget {
		t.Fatalf("p4 allocations = %.1f, budget = %.1f (serial %.1f)", parallel, parallelBudget, serial)
	}
	if absurd := serial*3 + 512; parallel > absurd {
		t.Fatalf("p4 allocations = %.1f, serial = %.1f, absurd-multiplier ceiling = %.1f", parallel, serial, absurd)
	}
}

func TestStackArenaOverflowKeepsExistingPointersStable(t *testing.T) {
	s := newStack()
	first := s.pushValue(storage{kind: stConst, typ: mtI32, cval: 1})
	for i := 0; i < defaultStackArenaCap+8; i++ {
		s.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(i + 2)})
	}
	if first.kind != ekValue || first.st.cval != 1 {
		t.Fatalf("first arena elem changed after overflow: kind=%v cval=%d", first.kind, first.st.cval)
	}
	if s.head.next != first {
		t.Fatal("first elem is no longer linked after arena overflow")
	}
	// Growing past the first chunk must advance the arena, never reallocate an
	// existing chunk (which would invalidate the pointers above).
	if len(s.chunks) < 2 {
		t.Fatalf("arena did not advance past the first chunk: %d chunk(s)", len(s.chunks))
	}
	if cap(s.chunks[0]) != defaultStackArenaCap {
		t.Fatalf("first chunk cap = %d, want %d", cap(s.chunks[0]), defaultStackArenaCap)
	}
}
