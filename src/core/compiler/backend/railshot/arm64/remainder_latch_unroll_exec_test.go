//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRemainderLatchUnrollExecution(t *testing.T) {
	if profileEnabled {
		t.Skip("native loop unrolling is disabled while profiling")
	}
	old := normalZeroBranchEnabled
	normalZeroBranchEnabled = true
	t.Cleanup(func() { normalZeroBranchEnabled = old })
	for _, wide := range []bool{false, true} {
		typ, eqz, rem := wasm.I32, byte(0x45), byte(0x70)
		fold := func(value uint64) uint64 { return uint64(uint32(value)) }
		if wide {
			typ, eqz, rem = wasm.I64, 0x50, 0x82
			fold = func(value uint64) uint64 { return value }
		}
		body := []byte{0, 0x3f, 0, 0x1a, 0x02, 0x40, 0x20, 1, eqz, 0x0d, 0, 0x03, 0x40,
			0x20, 0, 0x20, 1, 0x22, 0, rem, 0x22, 1, 0x0d, 0, 0x0b, 0x0b, 0x20, 0, 0x0b}
		m := modMem(t, 1, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, body)
		for _, enabled := range []bool{false, true} {
			for _, pair := range [][2]uint64{{0, 0}, {19, 0}, {48, 18}, {65535, 65534}, {0xfffffffffffa, 0xfffffffffff9}, {^uint64(0), 0x8000000000000001}} {
				a, b := fold(pair[0]), fold(pair[1])
				for b != 0 {
					a, b = b, a%b
				}
				var stats ModuleStats
				opts := CompileOptions{Optimizations: map[string]bool{"branch-fold": true, "scalar-loop-unroll": enabled}}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runArm64WrapperWithOptions(t, m, opts, pair[:]...)
				if err != nil || fold(got) != a {
					t.Fatalf("wide=%t enabled=%t pair=%x got=%x err=%v want=%x", wide, enabled, pair, got, err, a)
				}
				if diagnosticsEnabled && (stats.Funcs[0].Peephole["scalar-loop-unroll-remainder-latch"] != 0) != enabled {
					t.Fatalf("wide=%t enabled=%t path selection=%v", wide, enabled, stats.Funcs[0].Peephole)
				}
			}
		}
	}
}
