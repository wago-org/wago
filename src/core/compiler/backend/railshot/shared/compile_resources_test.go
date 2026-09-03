package shared

import "testing"

func TestCompileStageCountMatchesResourceLedger(t *testing.T) {
	var stats CompileResourceStats
	if got := len(stats.StageNanos); got != int(CompileStageCount) {
		t.Fatalf("stage slots = %d, want %d", got, CompileStageCount)
	}
}
