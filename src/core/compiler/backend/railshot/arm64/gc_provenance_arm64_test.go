//go:build arm64

package arm64

import (
	"testing"

	encoderarm64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

func TestGCProvenanceDoesNotUseSpillSlot(t *testing.T) {
	f := fn{a: &encoderarm64.Asm{}, s: newStack(), stats: &CodegenStats{}}
	e := elem{kind: ekValue, st: storage{kind: stSlot, typ: mtI64, slot: 1, gcRoot: true}}
	f.materialize(&e)
	if local, ok := gcLocalProvenance(&e); ok {
		t.Fatalf("spill slot inferred as local provenance: local %d", local)
	}
}

func TestGCProvenanceSurvivesPhysicalStorageChanges(t *testing.T) {
	f := fn{}
	e := elem{kind: ekValue, st: storage{kind: stLocalRef, typ: mtI64, idx: 3, gcRoot: true}}
	f.replaceStorage(&e, storage{kind: stSlot, typ: mtI64, slot: 1})
	f.occupy(&e, X0)
	if local, ok := gcLocalProvenance(&e); !ok || local != 3 {
		t.Fatalf("local provenance = %d, %v; want 3, true", local, ok)
	}
}
