//go:build amd64

package amd64

import "testing"

func TestSnapshotCallTypesReusesScratch(t *testing.T) {
	sc := newScratch()
	sc.tmpCallTypes = make([]machineType, 0, 65)
	f := fn{sc: sc}
	roots := make([]*elem, 65)
	for i := range roots {
		roots[i] = &elem{st: storage{typ: mtI32}}
	}
	roots[64] = &elem{kind: ekDeferred, st: storage{typ: mtNone}, typ: mtV128}

	got := f.snapshotCallTypes(roots)
	if len(got) != 65 || got[0] != mtI32 || got[64] != mtV128 {
		t.Fatalf("snapshot endpoints = (%v, %v), want (i32, v128)", got[0], got[64])
	}
	if allocs := testing.AllocsPerRun(100, func() { f.snapshotCallTypes(roots) }); allocs != 0 {
		t.Fatalf("steady-state allocations = %v, want 0", allocs)
	}
}

func TestScratchDropsPathologicalCallTypeCapacity(t *testing.T) {
	sc := newScratch()
	sc.tmpCallTypes = make([]machineType, 1, maxRetainedCallTypes+1)
	sc.reset()
	if sc.tmpCallTypes != nil {
		t.Fatalf("pathological call scratch retained capacity %d", cap(sc.tmpCallTypes))
	}

	sc.tmpCallTypes = make([]machineType, 1, maxRetainedCallTypes)
	sc.reset()
	if cap(sc.tmpCallTypes) != maxRetainedCallTypes || len(sc.tmpCallTypes) != 0 {
		t.Fatalf("ordinary call scratch = len %d cap %d", len(sc.tmpCallTypes), cap(sc.tmpCallTypes))
	}
}
