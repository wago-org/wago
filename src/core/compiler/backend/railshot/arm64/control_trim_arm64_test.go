//go:build (linux || darwin || windows) && arm64 && !tinygo

package arm64

import "testing"

func TestTrimControlScratchAfterShallowRunArm64(t *testing.T) {
	sc := newCompileScratch(defaultStackArenaCap)
	sc.ctrl = make([]ctrlFrame, 0, 640)
	sc.ctrlMerges = make([]ctrlFrameMerge, 0, 640)
	sc.ctrlRoots = make([]ctrlFrameRoots, 0, 640)
	frames := sc.ctrl[:cap(sc.ctrl)]
	merges := sc.ctrlMerges[:cap(sc.ctrlMerges)]
	roots := sc.ctrlRoots[:cap(sc.ctrlRoots)]
	frames[639].types = []machineType{mtI32}
	merges[639].eh = &ctrlFrameEH{}
	roots[639].flags = []bool{true}
	sc.trimControlScratch(8)
	if cap(sc.ctrl) != 640 || cap(sc.ctrlMerges) != 640 || cap(sc.ctrlRoots) != 640 {
		t.Fatal("first shallow function discarded outlier")
	}
	sc.trimControlScratch(8)
	if cap(sc.ctrl) != 0 || cap(sc.ctrlMerges) != 0 || cap(sc.ctrlRoots) != 0 {
		t.Fatal("second shallow function retained outlier")
	}
	if frames[639].types != nil || merges[639].eh != nil || roots[639].flags != nil {
		t.Fatal("released backing retained pointer-rich elements")
	}
	sc.ctrl = make([]ctrlFrame, 0, 64)
	sc.trimControlScratch(255)
	sc.trimControlScratch(0)
	if cap(sc.ctrl) != 64 || sc.smallControlRuns != 1 {
		t.Fatal("deep/small transition discarded ordinary capacity")
	}
	sc.ctrl[:cap(sc.ctrl)][5].mergeIndex = 5
	sc.ctrlMerges = make([]ctrlFrameMerge, 0, 640)
	sc.ctrlRoots = make([]ctrlFrameRoots, 0, 640)
	sc.trimControlScratch(0)
	if cap(sc.ctrl) != 64 || sc.ctrl[:cap(sc.ctrl)][5].mergeIndex != 0 {
		t.Fatal("retained frame kept stale merge index after sidecar release")
	}
}
