//go:build (linux || darwin) && arm64

package arm64

import "testing"

func TestBranchVectorComputedIndexesAndDefaults(t *testing.T) {
	for _, labels := range [][]uint32{{0, 1, 2, 3, 4, 5, 6, 7}, {7, 2, 5, 1, 6, 0, 4, 3}, {0, 0, 1, 2, 3, 4, 5, 6}} {
		for _, def := range []uint32{0, 8} {
			m := brTableComputedLabelsArm64(t, labels, def)
			for _, enabled := range []bool{false, true} {
				for _, compact := range []bool{false, true} {
					for _, guard := range []bool{false, true} {
						for _, index := range []uint64{0, 1, 2, 4, 7, 8, 9, 0x80000000, 0xffffffff} {
							var stats ModuleStats
							opts := CompileOptions{CompactNative: compact, ElideBoundsChecks: guard, Optimizations: map[string]bool{"br-table-branch-vector": enabled}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, index, 1)
							target := def
							if index < uint64(len(labels)) {
								target = labels[index]
							}
							want := uint64(1000 + target)
							if err != nil || got != want {
								t.Fatalf("labels=%v default=%d enabled=%v compact=%v guard=%v index=%x got=%d want=%d err=%v", labels, def, enabled, compact, guard, index, got, want, err)
							}
							if diagnosticsEnabled {
								admitted := enabled && !compact && !nativeCompactionEnabled && brTableSmallLabelsUnique(labels)
								if (stats.Funcs[0].Peephole["br-table-branch-vector"] != 0) != admitted {
									t.Fatalf("unexpected vector admission %+v", stats.Funcs[0].Peephole)
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestBranchVectorTargetExclusions(t *testing.T) {
	for _, tc := range []struct {
		name  string
		frame ctrlFrame
		want  bool
	}{
		{"block", ctrlFrame{kind: cfBlock}, true},
		{"loop", ctrlFrame{kind: cfLoop}, true},
		{"if", ctrlFrame{kind: cfIf}, true},
		{"function", ctrlFrame{kind: cfFunc}, false},
		{"block-result", ctrlFrame{kind: cfBlock, branchN: 1}, false},
		{"loop-parameter", ctrlFrame{kind: cfLoop, branchN: 1}, false},
		{"register-merge", ctrlFrame{kind: cfBlock, flags: ctrlRegMerge1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fn{ctrl: []ctrlFrame{tc.frame}}
			if got := f.branchVectorTarget(0); got != tc.want {
				t.Fatalf("target admission=%v want=%v", got, tc.want)
			}
			if f.branchVectorTarget(1) || f.branchVectorTarget(^uint32(0)) {
				t.Fatal("out-of-range target admitted")
			}
		})
	}
}
