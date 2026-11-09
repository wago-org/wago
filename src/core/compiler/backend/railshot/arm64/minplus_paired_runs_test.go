//go:build (linux || darwin) && arm64

package arm64

import "testing"

func TestMinPlusPairedRunsStateTrapsAndCaller(t *testing.T) {
	old := minPlusPairedRunsEnabled
	defer func() { minPlusPairedRunsEnabled = old }()
	for _, paired := range []bool{false, true} {
		minPlusPairedRunsEnabled = paired
		t.Run(map[bool]string{false: "original", true: "paired"}[paired], func(t *testing.T) {
			TestMinPlusRunsFullMemoryStateAndTraps(t)
			TestMinPlusRunsCallerFPAndVectors(t)
		})
	}
}

func TestMinPlusPairedRunsSelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	old, oldRuns := minPlusPairedRunsEnabled, minPlusRunsEnabled
	defer func() { minPlusPairedRunsEnabled, minPlusRunsEnabled = old, oldRuns }()
	minPlusRunsEnabled = true
	for _, paired := range []bool{false, true} {
		minPlusPairedRunsEnabled = paired
		stats := &ModuleStats{}
		cm, err := CompileModuleWith(minPlusRunsFixture(t, 0, 8192, 63, 0, 720), CompileOptions{Stats: stats})
		if err != nil {
			t.Fatal(err)
		}
		cm.CodeImage.Close()
		if got := stats.Funcs[0].Peephole["minplus-loop-paired-runs"]; (got != 0) != paired {
			t.Fatalf("paired%v selection%d", paired, got)
		}
	}
}
