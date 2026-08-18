//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

func findGuestStorageArrayType(t *testing.T, compiled *Compiled, storage gc.StorageKind, mutable bool) uint32 {
	t.Helper()
	for i, typ := range compiled.Types {
		if typ.Kind != CompositeTypeArray || typ.Array.Mutable != mutable || i >= len(compiled.GCTypeDescs) {
			continue
		}
		desc := compiled.GCTypeDescs[i]
		if desc.Kind == gc.KindArray && desc.Elem == storage {
			return uint32(i)
		}
	}
	t.Fatalf("no array type with storage %d mutable=%v", storage, mutable)
	return 0
}

func TestHostGuestStorageExactImmutableGCArrayResult(t *testing.T) {
	compiled, err := compileStagedGCArray(stagedGCArrayReferenceBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	typeIndex := findGuestStorageArrayType(t, compiled, gc.StorageI8, false)

	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	caller := in.beginHostCallScopeReservedWithID(newInvocationID(), nil)
	defer caller.scope.end(caller.generation, caller.parentGeneration)
	caller.exactResults = []ValueTypeDescriptor{{
		Kind: ValueTypeReference,
		Ref: ReferenceTypeDescriptor{
			Nullable: true,
			Heap:     HeapTypeDescriptor{Defined: true, TypeIndex: typeIndex},
		},
	}}

	want := []byte{1, 2, 3, 4, 5}
	token, err := caller.NewGCArrayResult(0, uint32(len(want)), func(payload []byte, info GuestGCArrayInfo) error {
		if info.Storage != GuestGCArrayI8 || info.Length != uint32(len(want)) || info.Mutable || info.TypeIndex != typeIndex {
			return &guestStorageTestError{"exact immutable GC result metadata"}
		}
		copy(payload, want)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if token == 0 {
		t.Fatal("host GC result allocation returned a null token")
	}
	defer func() {
		if err := in.ReleaseGCRef(GCRef{token: token}); err != nil {
			t.Errorf("release host GC result: %v", err)
		}
	}()

	var retained GuestStorage
	if err := caller.WithGuestStorage(func(storage GuestStorage) error {
		retained = storage
		resultType, ok := storage.ImportResultType(0)
		if !ok || resultType.Kind != ValueTypeReference || !resultType.Ref.Heap.Defined || resultType.Ref.Heap.TypeIndex != typeIndex {
			return &guestStorageTestError{"exact GC result type was not preserved"}
		}
		ref, err := storage.GCRef(token)
		if err != nil {
			return err
		}
		info, err := storage.GCArrayInfo(ref)
		if err != nil {
			return err
		}
		if info.Storage != GuestGCArrayI8 || info.Length != uint32(len(want)) || info.Mutable || info.TypeIndex != typeIndex {
			return &guestStorageTestError{"borrowed immutable GC array metadata"}
		}
		payload, payloadInfo, err := storage.GCArrayBytes(ref, GuestStorageRead)
		if err != nil {
			return err
		}
		if payloadInfo != info || !bytes.Equal(payload, want) {
			return &guestStorageTestError{"borrowed immutable GC array payload"}
		}
		if _, _, err := storage.GCArrayBytes(ref, GuestStorageWrite); err == nil {
			return &guestStorageTestError{"immutable GC array accepted a write borrow"}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if retained == nil {
		t.Fatal("GC test did not retain the test view")
	}
	if _, ok := retained.ImportResultType(0); ok {
		t.Fatal("expired GC guest-storage view still exposes import types")
	}
}
