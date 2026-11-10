//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcDynamicFillModule(storage byte, valueType wasm.ValType) []byte {
	bitsType := wasm.I32
	if storage == 0x7e || storage == 0x7c {
		bitsType = wasm.I64
	}
	get := []byte{0x23, 0, 0x20, 0, 0xfb, 11, 0}
	if storage == 0x78 || storage == 0x77 {
		get[5] = 13
	}
	if storage == 0x7d {
		get = append(get, 0xbc)
	} else if storage == 0x7c {
		get = append(get, 0xbd)
	}
	get = append(get, 0x0b)
	value := []byte{0x41, 42}
	switch storage {
	case 0x7e:
		value = []byte{0x42, 42}
	case 0x7d:
		value = []byte{0x43, 42, 0, 0, 0}
	case 0x7c:
		value = []byte{0x44, 42, 0, 0, 0, 0, 0, 0, 0}
	}
	run := []byte{0x03, 0x40, 0x23, 0, 0x41, 0}
	run = append(run, value...)
	run = append(run, 0x20, 1, 0xfb, 16, 0, 0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x23, 0, 0x41, 0, 0xfb, 11, 0)
	if storage == 0x78 || storage == 0x77 {
		run[len(run)-2] = 13
	}
	if storage == 0x7d {
		run = append(run, 0xbc)
	} else if storage == 0x7c {
		run = append(run, 0xbd)
	}
	run = append(run, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, storage, 1},
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, valueType, wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{bitsType}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{bitsType}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{3}, []byte{2}, []byte{4})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("fill", 0, 1), wasmtest.ExportEntry("get", 0, 2), wasmtest.ExportEntry("null", 0, 3), wasmtest.ExportEntry("run", 0, 4))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xfb, 7, 0, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x23, 0, 0x20, 0, 0x20, 1, 0x20, 2, 0xfb, 16, 0, 0x0b}),
			wasmtest.Code(get),
			wasmtest.Code([]byte{0xd0, 0, 0x20, 0, 0x20, 1, 0x20, 2, 0xfb, 16, 0, 0x0b}),
			wasmtest.Code(run))))
}

func TestGCDynamicNumericFill(t *testing.T) {
	for _, tc := range []struct {
		name        string
		storage     byte
		typ         wasm.ValType
		value, mask uint64
	}{
		{"i8", 0x78, wasm.I32, 0x1234ab, 0xff}, {"i16", 0x77, wasm.I32, 0x1234abcd, 0xffff},
		{"i32", 0x7f, wasm.I32, 0xfedcba98, 0xffffffff}, {"i64", 0x7e, wasm.I64, 0xfedcba9876543210, ^uint64(0)},
		{"f32", 0x7d, wasm.F32, 0x7fa12345, 0xffffffff}, {"f64", 0x7c, wasm.F64, 0x7ff0123456789abc, ^uint64(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDynamicFillModule(tc.storage, tc.typ))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
				in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, VerifyAfterCollect: true}})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := in.Invoke("init", 1100); err != nil {
					t.Fatal(err)
				}
				for _, count := range []uint64{0, 1, 3, 4, 7, 8, 15, 16, 17, 31, 32, 33, 64, 65, 127, 128, 129, 255, 256, 257, 258, 511, 512, 513, 515, 519, 1025} {
					if _, err := in.Invoke("fill", 0, 0, 1100); err != nil {
						t.Fatal(err)
					}
					if _, err := in.Invoke("fill", 3, tc.value, count); err != nil {
						t.Fatal(err)
					}
					for i := uint64(0); i < 1100; i++ {
						want := uint64(0)
						if i >= 3 && i < 3+count {
							want = tc.value & tc.mask
						}
						got, err := in.Invoke("get", i)
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("profile%d count%d index%d: %v,%v want%x", profile, count, i, got, err, want)
						}
					}
				}
				for _, bounds := range [][2]uint64{{1101, 0}, {1099, 2}, {0, 1<<32 - 1}, {1<<32 - 1, 2}} {
					if _, err := in.Invoke("fill", 0, 0, 1100); err != nil {
						t.Fatal(err)
					}
					_, fillErr := in.Invoke("fill", bounds[0], tc.value, bounds[1])
					var trap *TrapError
					if !errors.As(fillErr, &trap) || trap.Code != TrapBuiltin {
						t.Fatalf("bounds %v: %v", bounds, fillErr)
					}
					for i := uint64(0); i < 1100; i++ {
						got, err := in.Invoke("get", i)
						if err != nil || len(got) != 1 || got[0] != 0 {
							t.Fatalf("trap wrote payload: %v,%v", got, err)
						}
					}
				}
				if _, err := in.Invoke("fill", 1100, tc.value, 0); err != nil {
					t.Fatal(err)
				}
				if _, err := in.Invoke("null", 0, tc.value, 0); err == nil {
					t.Fatal("null zero fill accepted")
				}
				in.Close()
			}
		})
	}
}

func BenchmarkGCDynamicNumericFill(b *testing.B) {
	for _, count := range []int{4, 16, 64, 1024, 16384} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDynamicFillModule(0x7f, wasm.I32))
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			if _, err := in.Invoke("init", uint64(count)); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(count * 4 * 128))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err := in.Invoke("run", 128, uint64(count))
				if err != nil || len(got) != 1 || got[0] != 42 {
					b.Fatalf("run=%v,%v", got, err)
				}
			}
		})
	}
}

func BenchmarkGCDynamicNumericFillCompilation(b *testing.B) {
	binary := gcDynamicFillModule(0x7f, wasm.I32)
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c, err := Compile(cfg, binary)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(len(c.code)), "code-B")
		c.Close()
	}
}

func TestGCDynamicFillPreservesLiveValues(t *testing.T) {
	values := make([]wasm.ValType, 32)
	for i := range values {
		values[i] = wasm.I64
		if i < 20 {
			values[i] = wasm.F64
		}
	}
	params := append([]wasm.ValType{wasm.I32}, values...)
	results := append(append([]wasm.ValType(nil), values...), values...)
	body := []byte{}
	for i := range values {
		body = append(body, 0x20, byte(i+1))
	}
	body = append(body, 0x23, 0, 0x41, 0, 0x41, 42, 0x20, 0, 0xfb, 16, 0)
	for i := range values {
		body = append(body, 0x20, byte(i+1))
	}
	body = append(body, 0x0b)
	binary := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, wasmtest.FuncType(nil, nil), wasmtest.FuncType(params, results))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x80, 4, 0xfb, 7, 0, 0x24, 0, 0x0b}), wasmtest.Code(body))))
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), binary)
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
	args := make([]uint64, len(values)+1)
	for i := range values {
		args[i+1] = uint64(i)*0x123456789abc + 0x7ff0000000000001
	}
	for _, count := range []uint64{0, 4, 17, 64, 129, 512} {
		args[0] = count
		got, err := in.Invoke("run", args...)
		if err != nil || len(got) != 2*len(values) {
			t.Fatalf("count%d: %v,%v", count, got, err)
		}
		for i, value := range got {
			if value != args[i%len(values)+1] {
				t.Fatalf("count%d result%d=%x want%x", count, i, value, args[i%len(values)+1])
			}
		}
	}
}

// A corrupt physical extent must trap before the first store, even when the
// logical length admits the range. Restore the header before using the heap.
func TestGCDynamicNumericFillPhysicalExtent(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDynamicFillModule(0x7f, wasm.I32))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err = in.Invoke("init", 1100); err != nil {
		t.Fatal(err)
	}
	ref := uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type))
	view := in.gc.NativeView()
	handle := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(ref>>1)*corergc.NativeHandleStride)), corergc.NativeHandleStride)
	offset := binary.LittleEndian.Uint32(handle)
	space := handle[corergc.NativeHandleSpaceOffset]
	header := unsafe.Slice((*byte)(offHeapPtr(view.Spaces[space].Base+uintptr(offset))), corergc.PayloadOffset)
	saved := append([]byte(nil), header...)
	for _, count := range []uint64{1, 17, 129, 1025} {
		binary.LittleEndian.PutUint32(header[4:], uint32(corergc.PayloadOffset))
		_, fillErr := in.Invoke("fill", 3, 7, count)
		copy(header, saved)
		var trap *TrapError
		if !errors.As(fillErr, &trap) || trap.Code != TrapCastFailure {
			t.Fatalf("count%d: %v", count, fillErr)
		}
		for i := uint64(0); i < 1100; i++ {
			got, err := in.Invoke("get", i)
			if err != nil || len(got) != 1 || got[0] != 0 {
				t.Fatalf("count%d index%d: %v,%v", count, i, got, err)
			}
		}
	}
}
