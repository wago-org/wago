//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestBrTableDirectEmptyEdgesPreserveLabelsAndDirtyIndex(t *testing.T) {
	for _, labels := range [][]uint32{{}, {0}, {0, 1, 2}, {0, 0, 2}} {
		const def = uint32(3)
		m := brTableComputedLabelsArm64(t, labels, def)
		for _, enabled := range []bool{false, true} {
			for _, compact := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, index := range []uint32{0, 1, 2, 3, 4, 0x80000000, 0xffffffff} {
						var stats ModuleStats
						opts := CompileOptions{Workers: workers, CompactNative: compact, Optimizations: map[string]bool{"branch-fold": enabled}}
						if diagnosticsEnabled {
							opts.Stats = &stats
						}
						got, err := runArm64WrapperWithOptions(t, m, opts, uint64(index)|0xdeadbeef00000000, 1)
						label := def
						if uint64(index) < uint64(len(labels)) {
							label = labels[index]
						}
						if err != nil || got != uint64(1000+label) {
							t.Fatalf("labels=%v enabled=%t compact=%t workers=%d index=%x got=%d err=%v want=%d", labels, enabled, compact, workers, index, got, err, 1000+label)
						}
						if diagnosticsEnabled {
							want := 0
							if enabled {
								want = len(labels)
							}
							if stats.Funcs[0].Peephole["br-table-empty-edge"] != want {
								t.Fatalf("direct empty edge admission labels=%v enabled=%t stats=%v", labels, enabled, stats.Funcs[0].Peephole)
							}
						}
					}
				}
			}
		}
	}
}

func TestBrTableJumpUsesI32Index(t *testing.T) {
	// Repeated labels allow the compact target-ID form. Both jump-table
	// formats must ignore the high half of an incoming i32 carrier.
	labels := []byte{0, 1, 0, 1, 1, 0, 1, 0, 0, 0, 1, 1}
	body := []byte{0, 2, 0x40, 2, 0x40, 2, 0x40, 0x20, 0, 0x0e, byte(len(labels))}
	body = append(body, labels...)
	body = append(body, 2, 0x0b, 0x41, 10, 0x0f, 0x0b, 0x41, 20, 0x0f, 0x0b, 0x41, 30, 0x0b)
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, compact := range []bool{false, true} {
		for _, index := range []uint32{0, 1, 2, 4, 11, 12, ^uint32(0)} {
			want := uint64(30)
			if index < uint32(len(labels)) {
				want = 10 * uint64(labels[index]+1)
			}
			var stats ModuleStats
			opts := CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"br-table-branch-vector": false}}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			got, err := runArm64WrapperWithOptions(t, m, opts, uint64(index)|0xdeadbeef00000000)
			if err != nil || got != want {
				t.Fatalf("compact=%v index=%x: got %d, want %d, err=%v", compact, index, got, want, err)
			}
			if diagnosticsEnabled {
				peeps := stats.Funcs[0].Peephole
				if peeps["br-table-jump"] == 0 || (peeps["br-table-compact"] != 0) != compact {
					t.Fatalf("compact=%v: fixture used the wrong dispatch form: %v", compact, peeps)
				}
			}
		}
	}
}
