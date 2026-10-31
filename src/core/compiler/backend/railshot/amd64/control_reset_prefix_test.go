//go:build amd64

package amd64

import "testing"

func TestControlResetOnlyTracksTouchedSidecarPrefix(t *testing.T) {
	sc := newScratch()
	sc.ctrlMerges = make([]ctrlFrameMerge, 2048)
	f := fn{sc: sc}
	deep := ctrlFrame{mergeIndex: 2048}
	f.ctrlMerge(&deep).branchState = []locState{lsStackReg}
	f.ensureCtrlRoots(&deep).flags = []bool{true}
	sc.reset()
	if sc.ctrlMerges[2047].branchState != nil || sc.ctrlRoots[:cap(sc.ctrlRoots)][2047].flags != nil {
		t.Fatal("deep references survived reset")
	}
	shallow := ctrlFrame{mergeIndex: 1}
	f.ctrlMerge(&shallow).branchState = []locState{lsStackReg}
	f.ensureCtrlRoots(&shallow).flags = []bool{true}
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
