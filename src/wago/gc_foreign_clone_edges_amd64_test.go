//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"strings"
	"testing"
)

func foreignCloneEdgeModule() []byte {
	base := []byte{0x50, 0, 0x5f, 1, 0x7f, 1}
	sub := []byte{0x4f, 1, 0, 0x5f, 2, 0x7f, 1, 0x7e, 1}
	other := []byte{0x5f, 1, 0x7c, 1}
	parent := []byte{0x5f, 3, 0x63, 0, 1, 0x6d, 1, 0x64, 0, 1}
	array := []byte{0x5e, 0x63, 0, 1}
	fn := []byte{0x60, 0, 1, 0x63, 3}
	body := []byte{0, 0xfb, 1, 1, 0x41, 7, 0xfb, 0x1c, 0xfb, 1, 1, 0xfb, 0, 3, 0x0b}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(base, sub, other, parent, array, fn)), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(5))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("new", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}
func cloneEdgeRawRoot(t *testing.T, in *Instance, token GCRef) gc.Ref {
	t.Helper()
	in.refStore.mu.Lock()
	entry := in.refStore.gcByToken[token.token]
	in.refStore.mu.Unlock()
	ref, err := in.gc.CheckedGlobalSlot(entry.slot)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}
func TestForeignCloneValidatesTargetEdgeTypes(t *testing.T) {
	source := newForeignCloneBoundaryInstance(t, foreignCloneEdgeModule(), GCConfig{}, nil)
	target := newForeignCloneBoundaryInstance(t, foreignCloneEdgeModule(), GCConfig{CollectEveryAlloc: true, ForceMajorEveryMinor: true}, nil)
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

func TestForeignCloneArrayEdgeValidation(t *testing.T) {
	in := newForeignCloneBoundaryInstance(t, foreignCloneEdgeModule(), GCConfig{}, nil)
	good, err := in.gc.NewStructDefault(in.requireGCDomainType(1))
	if err != nil {
		t.Fatal(err)
	}
	bad, err := in.gc.NewStructDefault(in.requireGCDomainType(2))
	if err != nil {
		t.Fatal(err)
	}
	objects := []gcCloneObject{{typeID: 4, arrayLen: 1, values: []gcCloneValue{{kind: gc.StorageRefNull, ref: gcCloneRef{kind: gcCloneRefObject, value: 2}}}}, {typeID: 1}}
	if err = validateForeignGCCloneEdges(in, objects, gc.RefSliceRoots{0, good}); err != nil {
		t.Fatal(err)
	}
	if err = validateForeignGCCloneEdges(in, objects, gc.RefSliceRoots{0, bad}); err == nil {
		t.Fatal("wrong array element accepted")
	}
	objects[0].values[0].ref = gcCloneRef{kind: gcCloneRefObject, value: 3}
	if err = validateForeignGCCloneEdges(in, objects, gc.RefSliceRoots{0, good}); err == nil {
		t.Fatal("out-of-range clone edge accepted")
	}
}
