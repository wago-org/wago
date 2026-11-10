//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
	"unsafe"
)

func gcOpenStructAllocationModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x50, 0, 0x5f, 1, 0x7f, 1},
			[]byte{0x50, 1, 0, 0x5f, 2, 0x7f, 1, 0x6e, 1},
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{3})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 1, 1, 0xd0, 1, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("new", 0, 0), wasmtest.ExportEntry("inner", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 0, 0xfb, 0, 0, 0xfb, 0, 1, 0x24, 0, 0x23, 0, 0xfb, 2, 0, 0, 0x0b}),
			wasmtest.Code([]byte{0x23, 0, 0xfb, 2, 1, 1, 0xfb, 0x16, 0, 0xfb, 2, 0, 0, 0x0b}))))
}

func TestGCNativeOpenStructAllocation(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcOpenStructAllocationModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
		in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, StressNurseryBytes: 128, TinyHeapBytes: 256, TinyBlockBytes: 16, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		for i := uint64(0); i < 200; i++ {
			for _, name := range []string{"new", "inner"} {
				var got []uint64
				if name == "new" {
					got, err = in.Invoke(name, i)
				} else {
					got, err = in.Invoke(name)
				}
				if err != nil || len(got) != 1 || got[0] != i {
					t.Fatalf("profile %d %s(%d): %v, %v", profile, name, i, got, err)
				}
			}
		}
		if got := in.gc.Stats().Allocations; got != 400 {
			t.Fatalf("allocations %d, want400", got)
		}
		if err := in.CollectGC(); err != nil {
			t.Fatal(err)
		}
		if got, err := in.Invoke("inner"); err != nil || len(got) != 1 || got[0] != 199 {
			t.Fatalf("collected child: %v, %v", got, err)
		}
		if got := in.gc.Stats().LiveObjects; got != 2 {
			t.Fatalf("live objects %d, want2", got)
		}
		in.Close()
	}
}

func TestGCNativeOpenStructRespectsLargeObjectThreshold(t *testing.T) {
	for _, largeKind := range []byte{0x50, 0x4f} {
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x50, 0, 0x5f, 1, 0x7f, 0}, []byte{largeKind, 0, 0x5f, 2, 0x7e, 0, 0x7e, 0}, wasmtest.FuncType(nil, nil))),
			wasmtest.Section(3, wasmtest.Vec([]byte{2})),
			wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b}, []byte{0x63, 1, 1, 0xd0, 1, 0x0b})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 1, 0xfb, 0, 0, 0x24, 0, 0x42, 1, 0x42, 2, 0xfb, 0, 1, 0x24, 1, 0x0b}))))
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{NurseryBytes: 1024, SurvivorBytes: 512, LargeObjectBytes: 32, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if _, err := in.Invoke("run"); err != nil {
			t.Fatal(err)
		}
		ref := uint32(readGlobalObject(in.globalCells[1], in.c.Globals[1].Type))
		view := in.gc.NativeView()
		handle := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(ref>>1)*corergc.NativeHandleStride)), corergc.NativeHandleStride)
		if got := handle[corergc.NativeHandleSpaceOffset]; got != corergc.NativeSpaceLarge {
			t.Fatalf("32-byte object space=%d, want large=%d", got, corergc.NativeSpaceLarge)
		}
	}
}
