//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestDominatedIndexedBaseBranchMemory(t *testing.T) {
	// Initialize field8, read field4, optionally replace field8, then reread it.
	body := []byte{1, 1, 0x7f, 0x3f, 0, 0x1a,
		0x20, 0, 0x41, 3, 0x36, 2, 8,
		0x20, 0, 0x41, 11, 0x36, 2, 4,
		0x20, 0, 0x28, 2, 4, 0x21, 2,
		0x20, 1, 0x04, 0x40,
		0x20, 0, 0x41, 7, 0x36, 2, 8, 0x0b,
		0x20, 0, 0x28, 2, 8, 0x20, 2, 0x6a, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	for _, enabled := range []bool{false, true} {
		for _, guard := range []bool{false, true} {
			for _, ptr := range []uint64{0, 4, 64, 65520} {
				for _, flag := range []uint64{0, 1, 0xffffffff} {
					opts := CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"dominated-indexed-base": enabled}}
					got, err := runArm64WrapperWithOptions(t, m, opts, ptr, flag)
					want := uint64(14)
					if flag != 0 {
						want = 18
					}
					if err != nil || uint64(uint32(got)) != want {
						t.Fatalf("enabled=%v guard=%v ptr=%d flag=%d got=%d want=%d err=%v", enabled, guard, ptr, flag, got, want, err)
					}
				}
			}
		}
	}
}

func TestDominatedIndexedBaseLoopPaths(t *testing.T) {
	body := []byte{1, 2, 0x7f, 0x3f, 0, 0x1a,
		0x20, 0, 0x41, 11, 0x36, 2, 4,
		0x20, 0, 0x41, 3, 0x36, 2, 8,
		0x02, 0x40, 0x03, 0x40,
		0x20, 3, 0x20, 1, 0x4f, 0x0d, 1,
		0x20, 3, 0x41, 1, 0x71, 0x04, 0x40,
		0x20, 0, 0x41, 7, 0x36, 2, 8, 0x05,
		0x20, 0, 0x41, 3, 0x36, 2, 8, 0x0b,
		0x20, 2, 0x20, 0, 0x28, 2, 4, 0x6a,
		0x20, 0, 0x28, 2, 8, 0x6a, 0x21, 2,
		0x20, 3, 0x41, 1, 0x6a, 0x21, 3, 0x0c, 0,
		0x0b, 0x0b, 0x20, 2, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	for _, enabled := range []bool{false, true} {
		for _, guard := range []bool{false, true} {
			for _, ptr := range []uint64{0, 64, 65520} {
				for _, trips := range []uint64{0, 1, 2, 3, 9, 32} {
					opts := CompileOptions{ElideBoundsChecks: guard, Optimizations: map[string]bool{"dominated-indexed-base": enabled}}
					got, err := runArm64WrapperWithOptions(t, m, opts, ptr, trips)
					want := trips/2*32 + (trips%2)*14
					if err != nil || uint64(uint32(got)) != want {
						t.Fatalf("loop enabled=%v guard=%v ptr=%d trips=%d got=%d want=%d err=%v", enabled, guard, ptr, trips, got, want, err)
					}
				}
			}
		}
	}
}
