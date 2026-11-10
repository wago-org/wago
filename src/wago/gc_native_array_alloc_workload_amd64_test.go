//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"fmt"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcNativeDynamicDefaultArrayModule(count int) []byte {
	arrayType := []byte{0x5e, 0x7f, 0x01} // final (array (mut i32))
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x01, 0x01, 0x63, 0x00} // one (ref null type 0) local
	for range count {
		body = append(body,
			0x20, 0x00, // local.get 0: dynamic length
			0xfb, 0x07, 0x00, // array.new_default 0
			0x21, 0x01, // local.set 1
		)
	}
	body = append(body,
		0x20, 0x01, // local.get 1
		0xfb, 0x0f, // array.len
		0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func gcNativeDynamicTypedDefaultArrayModule(count int) []byte {
	types := [][]byte{
		{0x5f, 0x00},             // type 0: final empty struct
		{0x5e, 0x63, 0x00, 0x01}, // type 1: final (array (mut (ref null 0)))
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
	}
	body := []byte{0x00} // no locals
	for range count {
		body = append(body,
			0x20, 0x00, // local.get 0: dynamic length
			0xfb, 0x07, 0x01, // array.new_default 1
			0x24, 0x00, // global.set 0
		)
	}
	body = append(body,
		0x23, 0x00, // global.get 0
		0xfb, 0x0f, // array.len
		0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x01, 0x01, 0xd0, 0x01, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCNativeReferenceArrayAllocUsesExactNativeInitializers(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeReferenceArrayModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	const iterations = 128
	for i := 0; i < iterations; i++ {
		if _, err := in.Invoke("fixed"); err != nil {
			t.Fatal(err)
		}
		if _, err := in.Invoke("uniform"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGCNativeArrayDynamicAndLargeShapes(t *testing.T) {
	t.Run("dynamic", func(t *testing.T) {
		compiled, err := compileStagedGCArray(stagedGCArrayNumericLocalBytes(t))
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		in, err := instantiateCore(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()

		for i := uint64(0); i < 16; i++ {
			if got, err := in.Invoke("set_get", 3, 1, i); err != nil || len(got) != 1 || got[0] != i {
				t.Fatalf("set_get(%d) = %v, %v", i, got, err)
			}
		}
	})
	t.Run("large-static", func(t *testing.T) {
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeArrayDefaultBenchmarkModule([]byte{0x5e, 0x7f, 0x01}, 256))
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		in, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()

		if _, err := in.Invoke("run"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestGCNativeDynamicDefaultArrayUsesReservation(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeDynamicDefaultArrayModule(12))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	if got, err := in.Invoke("run", 4); err != nil || len(got) != 1 || got[0] != 4 {
		t.Fatalf("small dynamic array = %v, %v", got, err)
	}
	state := unsafe.Slice((*byte)(offHeapPtr(in.gc.NativeView().StructAllocState)), corergc.NativeStructAllocStateSize)
	if cursor := binary.LittleEndian.Uint32(state[corergc.NativeStructAllocCursorOffset:]); cursor == 0 {
		t.Fatal("dynamic default arrays did not consume a native reservation")
	}
	if got, err := in.Invoke("run", 256); err != nil || len(got) != 1 || got[0] != 256 {
		t.Fatalf("large dynamic array fallback = %v, %v", got, err)
	}
}

func TestGCNativeDynamicTypedDefaultArrayUsesLargeReservation(t *testing.T) {
	for _, length := range []uint64{4096, 8192} {
		t.Run(fmt.Sprintf("length=%d", length), func(t *testing.T) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeDynamicTypedDefaultArrayModule(2))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			in, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()

			if got, err := in.Invoke("run", length); err != nil || len(got) != 1 || got[0] != length {
				t.Fatalf("typed dynamic array = %v, %v", got, err)
			}
			state := unsafe.Slice((*byte)(offHeapPtr(in.gc.NativeView().LargeArrayAllocState)), corergc.NativeLargeArrayAllocStateSize)
			if count := binary.LittleEndian.Uint32(state[corergc.NativeStructAllocCountOffset:]); count != 1 {
				t.Fatalf("large reservation count = %d, want 1", count)
			}
			if cursor := binary.LittleEndian.Uint32(state[corergc.NativeStructAllocCursorOffset:]); cursor != 1 {
				t.Fatalf("large reservation cursor = %d, want 1 native allocation", cursor)
			}
			if got := in.gc.Stats().Allocations; got != 2 {
				t.Fatalf("semantic allocations = %d, want 2", got)
			}
		})
	}
}

func TestGCNativeArrayAllocPreservesAllocationCount(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeArrayDefaultBenchmarkModule([]byte{0x5e, 0x7f, 0x01}, 4))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	const iterations = 32
	for i := 0; i < iterations; i++ {
		got, err := in.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("iteration %d = %v, %v", i, got, err)
		}
	}
	const allocations = iterations * 33

	if got := in.gc.Stats().Allocations; got != uint64(allocations) {
		t.Fatalf("semantic allocations = %d, want %d", got, allocations)
	}
}

func TestGCNativeLargeArrayReservationRetainsCapacity(t *testing.T) {
	array := []byte{0x5e, 0x7f, 0x01}
	ft := wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)
	alloc := []byte{0x00, 0x20, 0x00, 0xfb, 0x07, 0x00, 0x24, 0x00, 0x0b}
	clear := []byte{0x00, 0xd0, 0x00, 0x24, 0x00, 0x0b}
	code := func(b []byte) []byte { return append(wasmtest.ULEB(uint32(len(b))), b...) }
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(array, ft, wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("alloc", 0, 0), wasmtest.ExportEntry("clear", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(code(alloc), code(clear))))
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprint("native=", native), func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-native-alloc", native), module)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{ThroughputHeapBytes: 256 << 10, ThroughputPageBytes: 4096, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for i := 0; i < 3; i++ {
				for _, bytes := range []uint64{60 << 10, 40 << 10} {
					if _, err := in.Invoke("alloc", (bytes-16)/4); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := in.Invoke("clear"); err != nil {
					t.Fatal(err)
				}
				if err := in.CollectGC(); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := in.Invoke("alloc", ((200<<10)-16)/4); err != nil {
				t.Fatalf("allocation after reclaim: %v; stats=%+v", err, in.gc.Stats())
			}
		})
	}
}
