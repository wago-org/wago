//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"reflect"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcMajorBlocksNativeModule() []byte {
	burst := []byte{0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x45, 0x0d, 1,
		0x20, 0, 0xfb, 0, 0, 0x24, 0, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0,
		0x0b, 0x0b, 0x23, 0, 0xfb, 2, 0, 0, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{3})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("burst", 0, 0), wasmtest.ExportEntry("last", 0, 1), wasmtest.ExportEntry("clear", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(burst), wasmtest.Code([]byte{0x23, 0, 0xfb, 2, 0, 0, 0x0b}), wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}))),
	)
}

func TestGCMajorBlocksNativeAllocationCycles(t *testing.T) {
	if !nativeStructAllocEnabled {
		t.Skip("native allocation is disabled")
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-native-alloc", true).WithOptimization("dead-gc-new", false), gcMajorBlocksNativeModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileThroughput, NurseryBytes: 2 << 20, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.Invoke("burst", 8191)
	if err != nil || len(got) != 1 || got[0] != 1 {
		t.Fatalf("warm native burst=%v,%v", got, err)
	}
	for i := 0; i < 2; i++ {
		if err := in.CollectGC(); err != nil {
			t.Fatal(err)
		}
	}
	got, err = in.Invoke("last")
	if err != nil || len(got) != 1 || got[0] != 1 || in.gc.Stats().LiveObjects != 1 {
		t.Fatal("native root did not survive enabling sparse index")
	}
	if _, err := in.Invoke("clear"); err != nil {
		t.Fatal(err)
	}
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
	// Admission only: keep the correctness oracle independent of this metadata.
	index := reflect.ValueOf(in.gc).Elem().FieldByName("majorBlocks")
	if !index.IsValid() || index.Len() == 0 {
		t.Fatal("sparse major index was not admitted")
	}
	// The first object after a collection refills the batch through Go; the
	// second object must consume a native reservation.
	for _, count := range []uint64{2, 512, 4608, 512} {
		got, err := in.Invoke("burst", count)
		if err != nil || len(got) != 1 || got[0] != 1 {
			t.Fatalf("burst%d=%v,%v", count, got, err)
		}
		state := in.gc.NativeView().StructAllocState
		if state == 0 || *(*uint32)(offHeapPtr(state + gc.NativeStructAllocCursorOffset)) == 0 {
			t.Fatal("fixture did not execute native allocation")
		}
		for i := 0; i < 2; i++ {
			if err := in.CollectGC(); err != nil {
				t.Fatal(err)
			}
		}
		if in.gc.Stats().LiveObjects != 1 {
			t.Fatalf("burst%d retained %d objects, want one", count, in.gc.Stats().LiveObjects)
		}
		got, err = in.Invoke("last")
		if err != nil || len(got) != 1 || got[0] != 1 {
			t.Fatalf("kept global=%v,%v", got, err)
		}
		if _, err := in.Invoke("clear"); err != nil {
			t.Fatal(err)
		}
		if err := in.CollectGC(); err != nil {
			t.Fatal(err)
		}
		if in.gc.Stats().LiveObjects != 0 {
			t.Fatalf("cleared burst%d retained %d objects", count, in.gc.Stats().LiveObjects)
		}
	}
}
