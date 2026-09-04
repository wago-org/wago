package shared

import "testing"

func TestCompileStageCountMatchesResourceLedger(t *testing.T) {
	var stats CompileResourceStats
	if got := len(stats.StageNanos); got != int(CompileStageCount) {
		t.Fatalf("stage slots = %d, want %d", got, CompileStageCount)
	}
}

func TestCompileResourceStatsAddWorkerScratch(t *testing.T) {
	var stats CompileResourceStats
	worker := WorkerScratchStats{
		NodeReserved: 1, NodePeak: 2, NodeRetained: 3, NodeDiscarded: 4,
		ControlReserved: 5, ControlPeak: 6, ControlRetained: 7, ControlDiscarded: 8,
	}
	stats.AddWorkerScratch(worker)
	stats.AddWorkerScratch(worker)
	if stats.NodeScratchReserved != 2 || stats.NodeScratchPeak != 4 || stats.NodeScratchRetained != 6 || stats.NodeScratchDiscarded != 8 ||
		stats.ControlScratchReserved != 10 || stats.ControlScratchPeak != 12 || stats.ControlScratchRetained != 14 || stats.ControlScratchDiscarded != 16 {
		t.Fatalf("merged worker scratch = %+v", stats)
	}
}
