//go:build linux && amd64 && !tinygo

package wago

import (
	"encoding/binary"
	"errors"
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcNumericFillModule(storage byte, valueType wasm.ValType, length byte) []byte {
	bitsType := wasm.I32
	if storage == 0x7e || storage == 0x7c {
		bitsType = wasm.I64
	}
	types := [][]byte{
		{0x5e, storage, 0x01},
		wasmtest.FuncType(nil, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, valueType}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{bitsType}),
	}
	bodies := [][]byte{{0, 0x41, length, 0xfb, 0x07, 0, 0x24, 0, 0x0b}}
	for _, count := range []byte{4, 16, 0, 17} {
		bodies = append(bodies, []byte{0, 0x23, 0, 0x20, 0, 0x20, 1, 0x41, count, 0xfb, 0x10, 0, 0x0b})
	}
	get := []byte{0, 0x23, 0, 0x20, 0, 0xfb, 0x0b, 0}
	if storage == 0x78 || storage == 0x77 {
		get[6] = 0x0d // packed unsigned load
	}
	if storage == 0x7d {
		get = append(get, 0xbc) // i32.reinterpret_f32
	} else if storage == 0x7c {
		get = append(get, 0xbd) // i64.reinterpret_f64
	}
	bodies = append(bodies, append(get, 0x0b))
	// Zero-length fill still traps for a null array. The value is an argument.
	bodies = append(bodies, []byte{0, 0xd0, 0, 0x20, 0, 0x20, 1, 0x41, 0, 0xfb, 0x10, 0, 0x0b})
	functionTypes := [][]byte{{1}, {2}, {2}, {2}, {2}, {3}, {2}}
	names := []string{"init", "fill4", "fill16", "zero", "large", "get", "null"}
	var exports, code [][]byte
	for i, body := range bodies {
		exports = append(exports, wasmtest.ExportEntry(names[i], 0, uint32(i)))
		code = append(code, append(wasmtest.ULEB(uint32(len(body))), body...))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(functionTypes...)),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(code...)),
	)
}

func TestGCNativeNumericFill(t *testing.T) {
	for _, tc := range []struct {
		name    string
		storage byte
		typ     wasm.ValType
		value   uint64
		mask    uint64
	}{
		{"i8", 0x78, wasm.I32, 0x1234ab, 0xff},
		{"i16", 0x77, wasm.I32, 0x1234abcd, 0xffff},
		{"i32", 0x7f, wasm.I32, 0xfedcba98, 0xffffffff},
		{"i64", 0x7e, wasm.I64, 0xfedcba9876543210, ^uint64(0)},
		{"f32", 0x7d, wasm.F32, 0x7fa12345, 0xffffffff},
		{"f64", 0x7c, wasm.F64, 0x7ff0123456789abc, ^uint64(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNumericFillModule(tc.storage, tc.typ, 20))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
				in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, VerifyAfterCollect: true}})
				if err != nil {
					t.Fatal(err)
				}
				check := func(start, count int, want uint64) {
					t.Helper()
					for i := 0; i < 20; i++ {
						got, err := in.Invoke("get", uint64(i))
						value := uint64(0)
						if i >= start && i < start+count {
							value = want & tc.mask
						}
						if err != nil || len(got) != 1 || got[0] != value {
							t.Fatalf("profile %d index %d: %v, %v; want %x", profile, i, got, err, value)
						}
					}
				}
				if _, err := in.Invoke("init"); err != nil {
					t.Fatal(err)
				}
				if _, err := in.Invoke("fill4", 2, tc.value); err != nil {
					t.Fatal(err)
				}
				check(2, 4, tc.value)
				for _, start := range []uint64{18, 0xffffffff} {
					if _, err := in.Invoke("fill4", start, 1); err == nil {
						t.Fatalf("fill4(%d) did not trap", start)
					}
					check(2, 4, tc.value)
				}
				if _, err := in.Invoke("zero", 20, tc.value); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"zero", "null"} {
					if _, err := in.Invoke(name, 21, tc.value); err == nil {
						t.Fatalf("%s did not trap", name)
					}
				}
				if _, err := in.Invoke("init"); err != nil {
					t.Fatal(err)
				}
				if _, err := in.Invoke("fill16", 4, tc.value); err != nil {
					t.Fatal(err)
				}
				check(4, 16, tc.value)
				if _, err := in.Invoke("init"); err != nil {
					t.Fatal(err)
				}
				if _, err := in.Invoke("large", 3, tc.value); err != nil {
					t.Fatal(err)
				}
				check(3, 17, tc.value)
				in.Close()
			}
		})
	}
}

func gcNumericSingleFillModule(length byte, sites int) []byte {
	types := [][]byte{{0x5e, 0x7f, 1}, wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil)}
	bodies := [][]byte{wasmtest.Code([]byte{0x41, length, 0xfb, 7, 0, 0x24, 0, 0x0b})}
	funcs := [][]byte{{1}}
	exports := [][]byte{wasmtest.ExportEntry("init", 0, 0)}
	for i := 0; i < sites; i++ {
		bodies = append(bodies, wasmtest.Code([]byte{0x23, 0, 0x20, 0, 0x20, 1, 0x41, 4, 0xfb, 0x10, 0, 0x0b}))
		funcs = append(funcs, []byte{2})
	}
	exports = append(exports, wasmtest.ExportEntry("fill4", 0, 1))
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(funcs...)), wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})), wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(bodies...)))
}

// Test the inline resolver with a short physical extent. Keep the handle and
// payload valid, and restore the header before inspecting or closing the heap.
func TestGCNativeNumericFillPreflight(t *testing.T) {
	for _, sites := range []int{1, 2} {
		for _, length := range []byte{0, 2, 20} {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNumericSingleFillModule(length, sites))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if _, err := in.Invoke("init"); err != nil {
				t.Fatal(err)
			}
			if length < 4 {
				if _, err := in.Invoke("fill4", 0, 7); err == nil {
					t.Fatalf("length %d accepted fill4", length)
				}
				continue
			}
			ref := uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type))
			view := in.gc.NativeView()
			handle := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(ref>>1)*corergc.NativeHandleStride)), corergc.NativeHandleStride)
			offset := binary.LittleEndian.Uint32(handle)
			space := handle[corergc.NativeHandleSpaceOffset]
			header := unsafe.Slice((*byte)(offHeapPtr(view.Spaces[space].Base+uintptr(offset))), corergc.PayloadOffset)
			saved := append([]byte(nil), header...)
			binary.LittleEndian.PutUint32(header[4:], 0)
			binary.LittleEndian.PutUint32(header[8:], ^uint32(0))
			_, fillErr := in.Invoke("fill4", 100, 7)
			copy(header, saved)
			var trap *TrapError
			if !errors.As(fillErr, &trap) || trap.Code != TrapCastFailure {
				t.Fatalf("invalid size: %v", fillErr)
			}
			for i := uint64(0); i < uint64(length); i++ {
				got, err := in.gc.ArrayGet(corergc.Ref(ref), uint32(i))
				if err != nil || got.Bits != 0 {
					t.Fatalf("failed fill changed element %d: %v, %v", i, got, err)
				}
			}
		}
	}
}

func TestGCNativeNumericZeroFillEvaluatesValue(t *testing.T) {
	types := [][]byte{{0x5e, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)}
	// null array, start 0, 1 / argument, count 0
	body := wasmtest.Code([]byte{0xd0, 0, 0x41, 0, 0x41, 1, 0x20, 0, 0x6e, 0x41, 0, 0xfb, 0x10, 0, 0x0b})
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("fill", 0, 0))), wasmtest.Section(10, wasmtest.Vec(body)))
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for _, arg := range []uint64{0, 1} {
		_, err := in.Invoke("fill", arg)
		var trap *TrapError
		want := TrapDivZero
		if arg != 0 {
			want = TrapNullReference
		}
		if !errors.As(err, &trap) || trap.Code != want {
			t.Fatalf("argument %d: %v; want %v", arg, err, want)
		}
	}
}

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
