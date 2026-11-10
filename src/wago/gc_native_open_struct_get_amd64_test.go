//go:build linux && amd64 && !tinygo

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

func gcOpenScalarReadModule(storage byte, valueType wasm.ValType, staticType uint32) []byte {
	bits := wasm.I32
	if valueType == wasm.I64 || valueType == wasm.F64 {
		bits = wasm.I64
	}
	// Equivalent root types 0 and 1 force canonical deduplication.
	fields := []byte{0x5f, 2, 0x7f, 1, storage, 1}
	types := [][]byte{append([]byte{0x50, 0}, fields...)}
	for i := byte(1); i <= 6; i++ {
		typ := []byte{0x50, 0}
		if i > 1 {
			typ = []byte{0x50, 1, i - 1}
		}
		types = append(types, append(typ, fields...))
	}
	types = append(types, append([]byte{0x4f, 0}, fields...))                // unrelated type 7
	types = append(types, wasmtest.FuncType([]wasm.ValType{valueType}, nil)) // type 8
	types = append(types, wasmtest.FuncType(nil, []wasm.ValType{bits}))      // type 9
	load := func(signed bool) []byte {
		op := byte(2)
		if storage == 0x78 || storage == 0x77 {
			op = 4
			if signed {
				op = 3
			}
		}
		code := []byte{0xfb, op, byte(staticType), 1}
		if valueType == wasm.F32 {
			code = append(code, 0xbc)
		}
		if valueType == wasm.F64 {
			code = append(code, 0xbd)
		}
		return code
	}
	init := []byte{0xfb, 1, 6, 0x24, 0, 0x23, 0, 0x20, 0, 0xfb, 5, byte(staticType), 1, 0x0b}
	read := append([]byte{0x23, 0}, load(false)...)
	read = append(read, 0x0b)
	signed := append([]byte{0x23, 0}, load(true)...)
	signed = append(signed, 0x0b)
	null := append([]byte{0xd0, byte(staticType)}, load(false)...)
	null = append(null, 0x0b)
	// Two local reads exercise the existing resolved-local certificate.
	repeat := []byte{1, 1, 0x63, byte(staticType), 0x23, 0, 0x21, 0, 0x20, 0}
	repeat = append(repeat, load(false)...)
	repeat = append(repeat, 0x20, 0)
	repeat = append(repeat, load(false)...)
	xor := byte(0x73)
	if bits == wasm.I64 {
		xor = 0x85
	}
	repeat = append(repeat, xor, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec([]byte{8}, []byte{9}, []byte{9}, []byte{9}, []byte{9}, []byte{8})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, byte(staticType), 1, 0xd0, byte(staticType), 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("read", 0, 1), wasmtest.ExportEntry("signed", 0, 2), wasmtest.ExportEntry("null", 0, 3), wasmtest.ExportEntry("repeat", 0, 4), wasmtest.ExportEntry("set", 0, 5))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(init), wasmtest.Code(read), wasmtest.Code(signed), wasmtest.Code(null), append(wasmtest.ULEB(uint32(len(repeat))), repeat...), wasmtest.Code([]byte{0x23, 0, 0x20, 0, 0xfb, 5, byte(staticType), 1, 0x0b}))),
	)
}

func TestGCNativeOpenStructScalarReads(t *testing.T) {
	for _, tc := range []struct {
		name        string
		storage     byte
		typ         wasm.ValType
		value, mask uint64
	}{
		{"i8", 0x78, wasm.I32, 0xab, 0xff}, {"i16", 0x77, wasm.I32, 0xabcd, 0xffff},
		{"i32", 0x7f, wasm.I32, 0xfedcba98, 0xffffffff}, {"i64", 0x7e, wasm.I64, 0xfedcba9876543210, ^uint64(0)},
		{"f32", 0x7d, wasm.F32, 0x7fa12345, 0xffffffff}, {"f64", 0x7c, wasm.F64, 0x7ff0123456789abc, ^uint64(0)},
	} {
		for _, staticType := range []uint32{1, 2, 6} {
			t.Run(fmt.Sprintf("%s/type%d", tc.name, staticType), func(t *testing.T) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcOpenScalarReadModule(tc.storage, tc.typ, staticType))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
					in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, VerifyAfterCollect: true}})
					if err != nil {
						t.Fatal(err)
					}
					localTypes := unsafe.Slice((*uint32)(offHeapPtr(in.gcNativeView.LocalTypes)), in.gcNativeView.LocalTypeCount)
					if localTypes[staticType] == staticType {
						t.Fatalf("test did not remap local type %d", staticType)
					}
					_, nullSetErr := in.Invoke("set", tc.value)
					var nullSetTrap *TrapError
					if !errors.As(nullSetErr, &nullSetTrap) || nullSetTrap.Code != TrapNullReference {
						t.Fatalf("null store: %v", nullSetErr)
					}
					for _, value := range []uint64{tc.value, 0, tc.value ^ 1} {
						if _, err := in.Invoke("init", value); err != nil {
							t.Fatal(err)
						}
						for round := 0; round < 2; round++ {
							for _, name := range []string{"read", "signed", "repeat"} {
								want := value & tc.mask
								if name == "signed" && tc.storage == 0x78 {
									want = uint64(uint32(int32(int8(value))))
								}
								if name == "signed" && tc.storage == 0x77 {
									want = uint64(uint32(int32(int16(value))))
								}
								if name == "repeat" {
									want = 0
								}
								got, err := in.Invoke(name)
								if err != nil || len(got) != 1 || got[0] != want {
									t.Fatalf("profile%d %s: %x, %v; want%x", profile, name, got, err, want)
								}
							}
							if err := in.CollectGC(); err != nil {
								t.Fatal(err)
							}
							if round == 0 {
								value ^= 2
								if _, err := in.Invoke("set", value); err != nil {
									t.Fatal(err)
								}
							}
						}
					}
					_, err = in.Invoke("null")
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != TrapNullReference {
						t.Fatalf("null: %v", err)
					}
					in.Close()
				}
			})
		}
	}
}

func TestGCNativeOpenStructReadPreflight(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcOpenScalarReadModule(0x7e, wasm.I64, 2))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init", 42); err != nil {
		t.Fatal(err)
	}
	ref := uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type))
	view := in.gc.NativeView()
	handle := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(ref>>1)*corergc.NativeHandleStride)), corergc.NativeHandleStride)
	offset := binary.LittleEndian.Uint32(handle)
	header := unsafe.Slice((*byte)(offHeapPtr(view.Spaces[handle[corergc.NativeHandleSpaceOffset]].Base+uintptr(offset))), corergc.PayloadOffset)
	check := func(name string, entryCheck bool, mutate func(), restore func()) {
		t.Helper()
		mutate()
		_, readErr := in.Invoke("read")
		_, setErr := in.Invoke("set", 43)
		restore()
		for _, err := range []error{readErr, setErr} {
			if entryCheck && nativeGCEntryValidationEnabled {
				if err == nil {
					t.Fatalf("%s: expected entry rejection", name)
				}
				continue
			}
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
				t.Fatalf("%s: %v", name, err)
			}
		}
		if got, err := in.Invoke("read"); err != nil || len(got) != 1 || got[0] != 42 {
			t.Fatalf("%s: failed store changed payload: %v, %v", name, got, err)
		}
	}
	savedHeader := append([]byte(nil), header...)
	savedHandle := append([]byte(nil), handle...)
	check("short header", false, func() { binary.LittleEndian.PutUint32(header[4:], corergc.HeaderSize) }, func() { copy(header, savedHeader) })
	check("short handle", false, func() { binary.LittleEndian.PutUint32(handle[corergc.NativeHandleSizeOffset:], corergc.HeaderSize) }, func() { copy(handle, savedHandle) })
	check("unknown type", false, func() { binary.LittleEndian.PutUint32(header, ^uint32(0)) }, func() { copy(header, savedHeader) })
	localTypes := unsafe.Slice((*uint32)(offHeapPtr(in.gcNativeView.LocalTypes)), in.gcNativeView.LocalTypeCount)
	for _, localType := range []int{1, 7} {
		check(fmt.Sprintf("incompatible type%d", localType), false, func() { binary.LittleEndian.PutUint32(header, localTypes[localType]) }, func() { copy(header, savedHeader) })
	}

	count := view.SubtypeIntervalCount
	check("interval range", true, func() { view.SubtypeIntervalCount = 0 }, func() { view.SubtypeIntervalCount = count })
	table := view.SubtypeIntervals
	check("missing intervals", true, func() { view.SubtypeIntervals = 0 }, func() { view.SubtypeIntervals = table })
	got, err := in.Invoke("read")
	if err != nil || len(got) != 1 || got[0] != 42 {
		t.Fatalf("restored read: %v, %v", got, err)
	}
}
