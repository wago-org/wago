//go:build (linux || darwin) && arm64 && !tinygo && !wago_guardpage

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func gcArrayNewFillFusionModule(start, count byte) []byte {
	arrayType := []byte{0x5e, 0x7f, 0x01} // final (array (mut i32))
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{
		0x01, 0x01, 0x63, 0x00, // one (ref null 0) local
		0x41, 0x00, 0x41, 0x10, 0xfb, 0x06, 0x00, 0x21, 0x02, // array.new 0 (0, 16); local.set 2
		0x20, 0x02, 0x41, start, 0x20, 0x00, 0x41, count, 0xfb, 0x10, 0x00, // fill from parameter 0
		0x20, 0x02, 0x20, 0x01, 0x41, 0x0f, 0x71, 0xfb, 0x0b, 0x00, // array.get at parameter 1 & 15
		0x0b,
	}
	body = append(wasmtest.ULEB(uint32(len(body))), body...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, funcType)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(body)),
	)
}

func gcArrayNewFillLocalStateModule() []byte {
	arrayType := []byte{0x5e, 0x7f, 0x01} // final (array (mut i32))
	funcType := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	body := func(dirty bool) []byte {
		code := []byte{0x02, 0x01, 0x63, 0x00, 0x01, 0x7f} // ref local 2, i32 local 3
		if dirty {
			code = append(code, 0x20, 0x00, 0x21, 0x03) // local 3 = parameter 0
			for range 8 {
				code = append(code, 0x20, 0x03, 0x1a) // make local 3 hot enough to pin
			}
		}
		code = append(code,
			0x41, 0x00, 0x41, 0x10, 0xfb, 0x06, 0x00, 0x21, 0x02,
			0x20, 0x02, 0x41, 0x00, 0x20, 0x03, 0x41, 0x10, 0xfb, 0x10, 0x00,
			0x20, 0x02, 0x20, 0x01, 0xfb, 0x0b, 0x00, 0x0b,
		)
		return append(wasmtest.ULEB(uint32(len(code))), code...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(arrayType, funcType)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0x01}, []byte{0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("dirty", 0, 0), wasmtest.ExportEntry("zero", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(body(true), body(false))),
	)
}

func TestGCArrayNewFillFusion(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcArrayNewFillFusionModule(0, 16))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	for _, gc := range []GCConfig{
		{},
		{CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true},
		{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true},
	} {
		instance, err := Instantiate(compiled, InstantiateOptions{GC: gc})
		if err != nil {
			t.Fatal(err)
		}
		for index := uint64(0); index < 16; index++ {
			value := uint64(uint32(0x81230000 + index))
			got, err := instance.Invoke("run", value, index)
			if err != nil || len(got) != 1 || got[0] != value {
				t.Fatalf("profile %d index %d = %v, %v; want [%#x]", gc.Profile, index, got, err, value)
			}
		}
		if got := instance.gc.Stats().Allocations; got != 16 {
			t.Fatalf("profile %d allocations = %d, want 16", gc.Profile, got)
		}
		if err := instance.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGCArrayNewFillFusionLocalState(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcArrayNewFillLocalStateModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	for _, gc := range []GCConfig{
		{},
		{CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true},
		{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true},
	} {
		instance, err := Instantiate(compiled, InstantiateOptions{GC: gc})
		if err != nil {
			t.Fatal(err)
		}
		for index := uint64(0); index < 16; index++ {
			value := uint64(uint32(0xc1230000 + index))
			got, err := instance.Invoke("dirty", value, index)
			if err != nil || len(got) != 1 || got[0] != value {
				t.Fatalf("profile %d dirty index %d = %v, %v; want [%#x]", gc.Profile, index, got, err, value)
			}
			got, err = instance.Invoke("zero", value, index)
			if err != nil || len(got) != 1 || got[0] != 0 {
				t.Fatalf("profile %d zero index %d = %v, %v; want [0]", gc.Profile, index, got, err)
			}
		}
		if got := instance.gc.Stats().Allocations; got != 32 {
			t.Fatalf("profile %d allocations = %d, want 32", gc.Profile, got)
		}
		if err := instance.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGCArrayNewFillFusionPreservesPartialFillAndBounds(t *testing.T) {
	for _, tc := range []struct {
		start, count byte
		trap         bool
	}{
		{0, 0, false}, {0, 8, false}, {8, 8, false}, {16, 0, false}, {0, 17, true}, {1, 16, true}, {17, 0, true},
	} {
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcArrayNewFillFusionModule(tc.start, tc.count))
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for index := uint64(0); index < 16; index++ {
			got, err := in.Invoke("run", 123, index)
			if tc.trap {
				if err == nil {
					t.Fatalf("fill %d+%d did not trap", tc.start, tc.count)
				}
				continue
			}
			want := uint64(0)
			if index >= uint64(tc.start) && index < uint64(tc.start)+uint64(tc.count) {
				want = 123
			}
			if err != nil || len(got) != 1 || got[0] != want {
				t.Fatalf("fill %d+%d index %d got=%v err=%v want=%d", tc.start, tc.count, index, got, err, want)
			}
		}
		in.Close()
		compiled.Close()
	}
}

// Keeping a second read live must preserve the object; the confined variant
// exercises repeated allocations and collection with a now-dead local.
func TestGCArrayNewFillConfinedLocal(t *testing.T) {
	for _, extraRead := range []bool{false, true} {
		body := []byte{2, 2, 0x7f, 1, 0x63, 0, // i32 counter/accumulator, nullable array
			0x03, 0x40, 0x20, 2, // loop; accumulator
			0x41, 0, 0x41, 16, 0xfb, 6, 0, 0x21, 3,
			0x20, 3, 0x41, 0, 0x20, 1, 0x41, 16, 0xfb, 16, 0,
			0x20, 3, 0x20, 1, 0x41, 15, 0x71, 0xfb, 11, 0,
		}
		if extraRead {
			body = append(body, 0x1a, 0x20, 3, 0x41, 0, 0xfb, 11, 0)
		}
		body = append(body, 0x6a, 0x21, 2,
			0x20, 1, 0x41, 1, 0x6a, 0x21, 1,
			0x20, 1, 0x20, 0, 0x49, 0x0d, 0, 0x0b, 0x20, 2, 0x0b)
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec([]byte{1})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		for _, cfg := range []GCConfig{{}, {CollectEveryAlloc: true, StressNurseryBytes: 256, VerifyAfterCollect: true}} {
			in, err := Instantiate(c, InstantiateOptions{GC: cfg})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			var allocations uint64
			for _, n := range []uint64{0, 1, 64, 4096, 2} {
				got, err := in.Invoke("run", n)
				if err != nil || len(got) != 1 || got[0] != n*(n-1)/2 {
					t.Fatalf("extra=%v n=%d result=%v err=%v", extraRead, n, got, err)
				}
				allocations += max(n, 1)
				if got := in.gc.Stats().Allocations; got != allocations {
					t.Fatalf("allocations=%d want=%d", got, allocations)
				}
				if err := in.CollectGC(); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
