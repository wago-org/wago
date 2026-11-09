//go:build (linux || darwin) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestOwnedResultSinkPreservesCapturedLocals(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, c, add, xor := wasm.I32, byte(0x41), byte(0x6a), byte(0x73)
		if wide {
			typ, c, add, xor = wasm.I64, 0x42, 0x7c, 0x85
		}
		for _, offset := range []byte{0, 1, 2, 7, 8, 9} {
			// Preserve the old b below a pending a^b. Writing a realizes that tree
			// into an owned register. The next binary operation assigns a different
			// pinned local and must preserve both the old b and the RHS c.
			body := []byte{0, 0x03, 0x7f - (c - 0x41), 0x20, 1, 0x20, 0, 0x20, 1, xor, 0x20, 0, c, 1, add, 0x21, 0,
				0x20, 2, add + offset, 0x21, 1, 0x20, 1, xor, 0x20, 2, xor, 0x0b, 0x0b}
			m := mod1(t, []wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, body)
			for _, enabled := range []bool{false, true} {
				for _, args := range [][3]uint64{{0, 0, 0}, {7, 5, 11}, {0xffffffff, 0x80000000, 0x12345678}, {0xfedcba9876543210, 0x8877665544332211, ^uint64(0)}} {
					a, b, c := args[0], args[1], args[2]
					v := a ^ b
					switch offset {
					case 0:
						v += c
					case 1:
						v -= c
					case 2:
						v *= c
					case 7:
						v &= c
					case 8:
						v |= c
					case 9:
						v ^= c
					}
					want := v ^ b ^ c
					if !wide {
						want = uint64(uint32(want))
					}
					opts := CompileOptions{Optimizations: map[string]bool{"three-op-sink": enabled}}
					var stats ModuleStats
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, args[:]...)
					if err != nil || got != want {
						t.Fatalf("wide=%v op=%x enabled=%v args=%x got=%x want=%x err=%v", wide, add+offset, enabled, args, got, want, err)
					}
					if diagnosticsEnabled {
						hits := stats.Funcs[0].Peephole["owned-result-sink"]
						if !enabled && hits != 0 || enabled && ownedResultSinkEnabled && offset == 9 && hits == 0 {
							t.Fatalf("cover selection mismatch: wide=%v op=%x enabled=%v hits=%d", wide, add+offset, enabled, hits)
						}
					}
				}
			}
		}
	}
}
