//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/wasmtest"
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

func guestStorageGCCollectModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x00, 0x20, 0x00, 0xfb, 0x00, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("roundtrip", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestCollectGCDuringGuestStorageFailsClosed(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), guestStorageGCCollectModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("roundtrip", 7); err != nil || len(got) != 1 || uint32(got[0]) != 7 {
		t.Fatalf("roundtrip = %v, %v; want [7], nil", got, err)
	}

	caller := in.beginHostCallScopeReservedWithID(newInvocationID(), nil)
	defer caller.scope.end(caller.generation, caller.parentGeneration)
	if err := caller.WithGuestStorage(func(GuestStorage) error {
		collectErr := in.CollectGC()
		if !errors.Is(collectErr, ErrPermissionDenied) {
			if collectErr != nil {
				return collectErr
			}
			return &guestStorageTestError{"collection during borrow was not rejected"}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := in.CollectGC(); err != nil {
		t.Fatalf("collection after guest-storage borrow: %v", err)
	}
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
	var resultTemps gcHostTempTokens
	caller.ephemeralGCResults = &resultTemps
	caller.exactResults = []ValueTypeDescriptor{{
		Kind: ValueTypeReference,
		Ref: ReferenceTypeDescriptor{
			Nullable: true,
			Heap:     HeapTypeDescriptor{Defined: true, TypeIndex: typeIndex},
		},
	}}

	want := []byte{1, 2, 3, 4, 5}
	token, err := caller.NewGCArrayResult(0, uint32(len(want)), func(payload []byte, info GuestGCArrayInfo) error {
		collectErr := in.CollectGC()
		if !errors.Is(collectErr, ErrPermissionDenied) {
			if collectErr != nil {
				return collectErr
			}
			return &guestStorageTestError{"collection during GC result initialization was not rejected"}
		}
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
	if resultTemps.count != 1 || resultTemps.tokens[0] != token {
		t.Fatalf("tracked host GC result = count %d token %#x, want 1/%#x", resultTemps.count, resultTemps.tokens[0], token)
	}

	var retained GuestStorage
	var retainedRef GuestGCRef
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
		if releaseErr := in.ReleaseGCRef(GCRef{token: token}); !errors.Is(releaseErr, ErrPermissionDenied) {
			if releaseErr != nil {
				return releaseErr
			}
			return &guestStorageTestError{"GC token release during borrow was not rejected"}
		}
		retainedRef = ref
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
		payload[0] = 0xff
		unchanged, _, err := storage.GCArrayBytes(ref, GuestStorageRead)
		if err != nil {
			return err
		}
		if !bytes.Equal(unchanged, want) {
			return &guestStorageTestError{"immutable GC array changed through read borrow"}
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
	if err := caller.WithGuestStorage(func(storage GuestStorage) error {
		if _, err := storage.GCArrayInfo(retainedRef); err == nil || !strings.Contains(err.Error(), "different guest-storage view") {
			return &guestStorageTestError{"GC reference crossed guest-storage views"}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	resultTemps.release(in)
	if resultTemps.count != 0 {
		t.Fatalf("released host GC result token count = %d, want 0", resultTemps.count)
	}
	if err := in.ReleaseGCRef(GCRef{token: token}); err == nil || !strings.Contains(err.Error(), "invalid or stale") {
		t.Fatalf("released host GC result token remained live: %v", err)
	}
}
