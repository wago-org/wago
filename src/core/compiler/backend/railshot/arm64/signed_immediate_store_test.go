//go:build arm64

package arm64

import (
	"fmt"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Require an actual bounds proof regardless of whether the native CPU happens
// to leave memory unchanged when a single unaligned store faults.
func TestGuardedConstantI64StoreBoundsProof(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, value := range []int64{0, -1, math.MinInt32, math.MaxInt32 + 1, math.MinInt64} {
		for _, offset := range []uint32{0, 7, 128, math.MaxInt32, math.MaxUint32} {
			t.Run(fmt.Sprintf("value=%x/offset=%d", uint64(value), offset), func(t *testing.T) {
				m, err := wasm.DecodeModule(wasmtest.SignedImmediateStore(value, 8, false, false, false, offset))
				if err != nil {
					t.Fatal(err)
				}
				if err := wasm.ValidateModule(m); err != nil {
					t.Fatal(err)
				}
				var stats ModuleStats
				cm, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: true, Stats: &stats})
				if err != nil {
					t.Fatal(err)
				}
				defer cm.CodeImage.Close()
				if len(stats.Funcs) != 1 {
					t.Fatalf("function statistics count = %d, want 1", len(stats.Funcs))
				}
				if got := stats.Funcs[0].BoundsChecks; got != 1 {
					t.Fatalf("guarded i64 constant store bounds checks = %d, want 1", got)
				}
			})
		}
	}
}

func TestGuardedConstantStoreBoundsScope(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, guard := range []bool{false, true} {
		for _, size := range []int{1, 2, 4, 8} {
			t.Run(fmt.Sprintf("guard=%t/size=%d", guard, size), func(t *testing.T) {
				m, err := wasm.DecodeModule(wasmtest.SignedImmediateStore(-1, size, false, false, false, 7))
				if err != nil {
					t.Fatal(err)
				}
				var stats ModuleStats
				cm, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: guard, Stats: &stats})
				if err != nil {
					t.Fatal(err)
				}
				defer cm.CodeImage.Close()
				want := 1
				if guard && size != 8 {
					want = 0
				}
				if len(stats.Funcs) != 1 {
					t.Fatalf("function statistics count = %d, want 1", len(stats.Funcs))
				}
				if got := stats.Funcs[0].BoundsChecks; got != want {
					t.Fatalf("bounds checks = %d, want %d", got, want)
				}
			})
		}
	}
}
