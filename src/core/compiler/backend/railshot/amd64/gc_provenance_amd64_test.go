//go:build amd64

package amd64

import "testing"

func TestGCProvenanceDoesNotUseSpillSlot(t *testing.T) {
	f := fn{}
	e := elem{kind: ekValue, st: storage{kind: stSlot, typ: mtI64, slot: 1, gcRoot: true}}
	f.occupy(&e, RAX)
	if local, ok := gcLocalProvenance(&e); ok {
		t.Fatalf("spill slot inferred as local provenance: local %d", local)
	}
}

func TestGCProvenanceSurvivesPhysicalStorageChanges(t *testing.T) {
	f := fn{}
	e := elem{kind: ekValue, st: storage{kind: stLocalRef, typ: mtI64, idx: 3, gcRoot: true}}
	f.replaceStorage(&e, storage{kind: stSlot, typ: mtI64, slot: 1})
	f.occupy(&e, RAX)
	if local, ok := gcLocalProvenance(&e); !ok || local != 3 {
		t.Fatalf("local provenance = %d, %v; want 3, true", local, ok)
	}
}
