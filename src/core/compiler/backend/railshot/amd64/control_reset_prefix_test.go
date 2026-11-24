//go:build amd64

package amd64

import "testing"

func TestControlResetOnlyTracksTouchedSidecarPrefix(t *testing.T) {
	sc := newScratch()
	sc.ctrlMerges = make([]ctrlFrameMerge, 2048)
	f := fn{sc: sc}
	deep := ctrlFrame{mergeIndex: 2048}
	f.ensureCtrlMerge(&deep).branchState = []locState{lsStackReg}
	f.ensureCtrlRoots(&deep).flags = []bool{true}
	sc.reset()
	if sc.ctrlMerges[2047].branchState != nil || sc.ctrlRoots[:cap(sc.ctrlRoots)][2047].flags != nil {
		t.Fatal("deep references survived reset")
	}
	shallow := ctrlFrame{mergeIndex: 1}
	f.ensureCtrlMerge(&shallow).branchState = []locState{lsStackReg}
	f.ensureCtrlRoots(&shallow).flags = []bool{true}
	// Probing a cleared deep slot must not expand the reset window.
	if roots := f.ctrlRoots(&deep); f.ctrlMerge(&deep).branchState != nil || roots != nil && roots.flags != nil {
		t.Fatal("read of cleared deep sidecar returned stale references")
	}
	if sc.ctrlMergeTouched != 1 {
		t.Fatalf("shallow function touched %d slots", sc.ctrlMergeTouched)
	}
	sc.reset()
	if sc.ctrlMerges[0].branchState != nil || sc.ctrlRoots[:cap(sc.ctrlRoots)][0].flags != nil {
		t.Fatal("shallow references survived reset")
	}
	if sc.ctrlMergeTouched != 0 {
		t.Fatal("reset retained touched prefix")
	}
}
