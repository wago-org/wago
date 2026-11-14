//go:build arm64

package arm64

import "testing"

func TestBranchTableRunsPreserveComputedIndexAndDefault(t *testing.T) {
	saved := brTableRunsEnabled
	defer func() { brTableRunsEnabled = saved }()
	tables := [][]uint32{
		{0, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 1},
		{0, 0, 0, 0, 0, 0, 0, 0},
		{2, 2, 2, 2, 0, 0, 0, 0, 2, 2, 2, 2},
		{0, 0, 1, 1, 2, 2, 3, 3}, // four runs retain ordinary dispatch
	}
	for table, labels := range tables {
		for _, def := range []uint32{0, 1, 2, 4} {
			m := brTableComputedLabelsArm64(t, labels, def)
			for _, enabled := range []bool{false, true} {
				for _, compact := range []bool{false, true} {
					brTableRunsEnabled = enabled
					for i := uint64(0); i < uint64(len(labels))+4; i++ {
						for _, input := range []uint64{i, i | 0xffffffff00000000, 0xffffffff, 0x80000000} {
							var stats ModuleStats
							opts := CompileOptions{CompactNative: compact}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, input, 1)
							label := def
							index := uint32(input)
							if uint64(index) < uint64(len(labels)) {
								label = labels[index]
							}
							if err != nil || got != uint64(1000+label) {
								t.Fatalf("table=%d def=%d enabled=%v compact=%v input=%x got=%d want=%d err=%v", table, def, enabled, compact, input, got, 1000+label, err)
							}
							if diagnosticsEnabled {
								want := enabled && !compact && !nativeCompactionEnabled && table < 3
								if (stats.Funcs[0].Peephole["br-table-runs"] != 0) != want {
									t.Fatalf("unexpected admission table=%d enabled=%v compact=%v stats=%v", table, enabled, compact, stats.Funcs[0].Peephole)
								}
							}
						}
					}
				}
			}
		}
	}
}
