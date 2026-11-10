//go:build (linux || darwin) && arm64

package arm64

import "testing"

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
