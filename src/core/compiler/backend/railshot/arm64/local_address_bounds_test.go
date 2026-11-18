//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestLocalAddressBoundsAssignmentsArm64(t *testing.T) {
	mask := func(v int32) []byte {
		b := []byte{0x20, 0, 0x41}
		b = append(b, wasmtest.SLEB32(v)...)
		return append(b, 0x71)
	}
	set := func(b []byte) []byte { return append(b, 0x21, 1) }
	for _, tc := range []struct {
		name    string
		writes  []byte
		address byte
		proof   bool
		traps   bool
	}{
		{"zero initializer", nil, 1, true, false},
		{"masked", set(mask(63)), 1, true, false},
		{"masked shifted", set(append(mask(63), 0x41, 3, 0x74)), 1, true, false},
		{"unknown later write", append(set(mask(63)), 0x20, 0, 0x21, 1), 1, false, true},
		{"unknown earlier write", append([]byte{0x20, 0, 0x21, 1}, set(mask(63))...), 1, false, false},
		{"unknown conditional write", append(set(mask(63)), 0x20, 0, 0x04, 0x40, 0x20, 0, 0x21, 1, 0x0b), 1, false, true},
		{"parameter", nil, 0, false, true},
		{"access crosses minimum", set(mask(65535)), 1, false, true},
		{"overflowing shift", set(append(mask(-1), 0x41, 1, 0x74)), 1, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := append([]byte{1, 1, 0x7f}, tc.writes...)
			body = append(body, 0x20, tc.address, 0x28, 0, 0, 0x0b)
			m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
			for _, enabled := range []bool{false, true} {
				for _, arg := range []uint32{0, 1, 7, ^uint32(0)} {
					var stats ModuleStats
					opts := CompileOptions{Optimizations: map[string]bool{"memory-min-bounds": enabled, "bounds-facts": false}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperMemWithOptions(t, m, opts, arg, func(mem []byte) {
						for i := range mem {
							mem[i] = 0x5a
						}
					})
					wantTrap := tc.traps && arg == ^uint32(0)
					if wantTrap {
						if err == nil {
							t.Fatalf("enabled=%v arg=%x: missing trap", enabled, arg)
						}
					} else if err != nil || got != 0x5a5a5a5a {
						t.Fatalf("enabled=%v arg=%x: got=%x err=%v", enabled, arg, got, err)
					}
					if diagnosticsEnabled {
						want := 0
						if enabled && tc.proof {
							want = 1
						}
						if hits := stats.Funcs[0].Peephole["memory-min-bounds"]; hits != want {
							t.Fatalf("enabled=%v: bounds proofs=%d want=%d", enabled, hits, want)
						}
					}
				}
			}
		})
	}
}
