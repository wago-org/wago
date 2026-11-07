//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestLocalSquareSnapshotPreservesOldValueAcrossAssignment(t *testing.T) {
	old := localSquareSnapshotEnabled
	t.Cleanup(func() { localSquareSnapshotEnabled = old })
	for _, wide := range []bool{false, true} {
		localType := byte(0x7f)
		vt, constOp, mul, add, xor := wasm.I32, byte(0x41), byte(0x6c), byte(0x6a), byte(0x73)
		if wide {
			localType = 0x7e
			vt, constOp, mul, add, xor = wasm.I64, 0x42, 0x7e, 0x7c, 0x85
		}
		for _, nested := range []bool{false, true} {
			body := []byte{1, 2, localType, 0x3f, 0, 0x1a, 0x20, 0, 0x21, 1, 0x03, 0x40}
			if nested {
				body = append(body, constOp, 3)
			}
			body = append(body, 0x20, 1, 0x20, 1, mul)
			if nested {
				body = append(body, add)
			}
			body = append(body, 0x20, 1, constOp, 17, add, 0x21, 1, 0x20, 1, xor, 0x21, 2, 0x41, 0, 0x0d, 0, 0x0b, 0x20, 2, 0x0b)
			m := modMem(t, 1, []wasm.ValType{vt}, []wasm.ValType{vt}, body)
			for _, enabled := range []bool{false, true} {
				localSquareSnapshotEnabled = enabled
				for _, n := range []uint64{0, 1, 17, 0x7fffffff, 0x80000000, 0xffffffff, 0x123456789abcdef0, 0x8000000000000000, ^uint64(0)} {
					want := n * n
					if nested {
						want += 3
					}
					want ^= n + 17
					if !wide {
						want = uint64(uint32(want))
						n |= 0xdeadbeef00000000
					}
					var stats ModuleStats
					opts := CompileOptions{Optimizations: map[string]bool{"linear-integer-tree": false, "interval-region-pins": false, "loop-int-const": false}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					got, err := runArm64WrapperWithOptions(t, m, opts, n)
					if err != nil || got != want {
						t.Fatalf("wide=%v nested=%v enabled=%v n=%x got=%x want=%x err=%v", wide, nested, enabled, n, got, want, err)
					}
					if diagnosticsEnabled && (stats.Funcs[0].Peephole["local-square-snapshot"] > 0) != enabled {
						t.Fatalf("enabled=%v stats=%v", enabled, stats.Funcs[0].Peephole)
					}
				}
			}
		}
	}
}
