//go:build amd64

package amd64

import (
	"testing"
	"unsafe"
)

func TestCtrlFrameSize(t *testing.T) {
	if got, want := unsafe.Sizeof(ctrlFrame{}), uintptr(312); got != want {
		t.Fatalf("ctrlFrame size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(ctrlFrameMerge{}), uintptr(96); got != want {
		t.Fatalf("ctrlFrameMerge size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(ctrlFrameEH{}), uintptr(48); got != want {
		t.Fatalf("ctrlFrameEH size = %d, want %d", got, want)
	}
}

func TestPushCtrlReusesMergeSlotAtDepth(t *testing.T) {
	f := fn{ctrl: make([]ctrlFrame, 0, 1)}
	first := ctrlFrame{}
	f.ensureCtrlMerge(&first).branchState = make([]locState, 1)
	f.pushCtrl(&first)
	f.releaseCtrlMerge(&f.ctrl[0])
	f.ctrl = f.ctrl[:0]

	next := ctrlFrame{}
	f.ensureCtrlMerge(&next).branchState = make([]locState, 2)
	f.pushCtrl(&next)

	if got, want := len(f.scratchState().ctrlMerges), 1; got != want {
		t.Fatalf("merge sidecar length = %d, want %d", got, want)
	}
	if got, want := f.ctrl[0].mergeIndex, uint32(1); got != want {
		t.Fatalf("merge index = %d, want %d", got, want)
	}
	if got, want := next.mergeIndex, uint32(1); got != want {
		t.Fatalf("caller merge index = %d, want %d", got, want)
	}
	if got, want := len(f.frameBranchState(&f.ctrl[0])), 2; got != want {
		t.Fatalf("moved branch state length = %d, want %d", got, want)
	}
	f.releaseCtrlMerge(&next)
	if got := f.frameBranchState(&f.ctrl[0]); got != nil {
		t.Fatalf("released branch state = %v, want nil", got)
	}
}

func TestGCRootFlagsAvoidsAllFalseBacking(t *testing.T) {
	roots := []*elem{{kind: ekValue}, {kind: ekDeferred}, {kind: ekValue}}
	if got := gcRootFlags(roots); got != nil {
		t.Fatalf("all-false roots = %v, want nil", got)
	}
	roots[1].kind = ekValue
	roots[1].st.gcRoot = true
	got := gcRootFlags(roots)
	if len(got) != len(roots) || got[0] || !got[1] || got[2] {
		t.Fatalf("roots = %v, want [false true false]", got)
	}
}
