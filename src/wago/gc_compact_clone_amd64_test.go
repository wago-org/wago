//go:build linux && amd64 && (wago_gc_compact_helpers || (wago_gc_compact_reads || (wago_gc_compact_alloc || ((wago_gc_compact_refstore || wago_gc_compact_shared_checks) || (wago_gc_compact_scalar_arrays || (wago_gc_compact_array_alloc || (wago_gc_compact_fill || (wago_gc_compact_copy || (wago_gc_compact_loop || wago_gc_compact_pair_loop || (wago_gc_compact_range_loop || wago_gc_compact_range_sse2) || wago_gc_compact_range_stride)))))))) || wago_gc_compact_refstruct)

package wago

import (
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"strings"
	"testing"
)

func TestCompactGCCloneValidatesMovingEdges(t *testing.T) {
	source := newForeignCloneBoundaryInstance(t, foreignCloneEdgeModule(), GCConfig{ExperimentalCompactOffsets: true}, nil)
	target := newForeignCloneBoundaryInstance(t, foreignCloneEdgeModule(), GCConfig{ExperimentalCompactOffsets: true, CollectEveryAlloc: true, ForceMajorEveryMinor: true}, nil)
	token := foreignCloneSourceToken(t, source)
	// A declared subtype, i31 in eqref, and nullable null remain valid while
	// reconstruction roots move at every target allocation.
	cloned, err := target.CloneGCRefFrom(source, token)
	if err != nil {
		t.Fatal(err)
	}
	if err = target.ReleaseGCRef(cloned); err != nil {
		t.Fatal(err)
	}
	root := cloneEdgeRawRoot(t, source, token)
	if err = source.gc.StructSet(root, 0, gc.RefValue(gc.Null())); err != nil {
		t.Fatal(err)
	}
	cloned, err = target.CloneGCRefFrom(source, token)
	if err != nil {
		t.Fatal(err)
	}
	if err = target.ReleaseGCRef(cloned); err != nil {
		t.Fatal(err)
	}
	wrong, err := source.gc.NewStructDefaultWithRoots(source.requireGCDomainType(2), gc.EmptyRoots{})
	if err != nil {
		t.Fatal(err)
	}
	root = cloneEdgeRawRoot(t, source, token)
	for _, bad := range []gc.Ref{wrong, gc.I31New(1)} {
		// The raw collector validates live/kind/nullability, not Wasm subtyping.
		if err = source.gc.StructSet(root, 0, gc.RefValue(bad)); err != nil {
			t.Fatal(err)
		}
		if _, err = target.CloneGCRefFrom(source, token); err == nil || !strings.Contains(err.Error(), "target reference type") {
			t.Fatalf("wrong edge accepted: %v", err)
		}
	}
	if err = source.gc.StructSet(root, 0, gc.RefValue(gc.Null())); err != nil {
		t.Fatal(err)
	}
	objects, encoded, err := captureForeignGCGraph(source, token.token, target)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a source collector returning a null in a non-null field. No token
	// may be published even though reconstruction itself uses nullable shells.
	objects[0].values[2].ref = gcCloneRef{}
	if _, _, err = restoreForeignGCGraph(target, objects, encoded); err == nil || !strings.Contains(err.Error(), "target reference type") {
		t.Fatalf("non-null edge accepted: %v", err)
	}
}
