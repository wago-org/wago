//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Keep references, parameters, and more field results than XMM registers below
// each cached read. Returning raw float values also checks signaling NaN bits.
func gcCachedReadPressureModule(typ wasm.ValType) []byte {
	storage := byte(0x7c)
	if typ == wasm.F32 {
		storage = 0x7d
	}
	params := make([]wasm.ValType, 32)
	for i := range params {
		params[i] = wasm.I64
		if i < 20 {
			params[i] = typ
		}
	}
	results := []wasm.ValType{wasm.EqRef, wasm.EqRef}
	results = append(results, params...)
	// More than 64 locals uses the compiler's conservative local-pin policy,
	// leaving a register for the address certificate under operand pressure.
	body := []byte{1, 33, 0x63, 0, 0x23, 0, 0x21, 32, 0x23, 1, 0x21, 33,
		0x20, 32, 0xfb, 2, 0, 0, 0x1a, 0x20, 32, 0x20, 33}
	for i := range params {
		body = append(body, 0x20, byte(i))
	}
	for i := 0; i < 24; i++ {
		body = append(body, 0x20, 32, 0xfb, 2, 0, 0)
		results = append(results, typ)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, storage, 1}, wasmtest.FuncType([]wasm.ValType{typ}, nil), wasmtest.FuncType(params, results))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b}, []byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xfb, 0, 0, 0x24, 0, 0x20, 0, 0xfb, 0, 0, 0x24, 1, 0x0b}), append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCCachedReadFloatPressure(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.F32, wasm.F64} {
		t.Run(fmt.Sprint(typ), func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcCachedReadPressureModule(typ))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
				in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, VerifyAfterCollect: true}})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				value := uint64(0x7ff0123456789abc)
				if typ == wasm.F32 {
					value = 0x7fa12345
				}
				if _, err := in.Invoke("init", value); err != nil {
					t.Fatal(err)
				}
				args := make([]uint64, 32)
				for i := range args {
					args[i] = uint64(i)*0x102030405 + 17
					if i < 20 {
						args[i] = value + uint64(i)
					}
				}
				for round := 0; round < 2; round++ {
					want := []uint64{readGlobalObject(in.globalCells[0], in.c.Globals[0].Type), readGlobalObject(in.globalCells[1], in.c.Globals[1].Type)}
					want = append(want, args...)
					for i := 0; i < 24; i++ {
						want = append(want, value)
					}
					got, err := in.Invoke("run", args...)
					if err != nil || len(got) != len(want) {
						t.Fatalf("profile%d: %x, %v", profile, got, err)
					}
					for i := range want {
						actual := got[i]
						if i < 2 {
							entry, ok := in.refStore.gcByToken[actual]
							if !ok || entry.owner != in {
								t.Fatalf("invalid reference token %x", actual)
							}
							actual = uint64(entry.ref)
						}
						if actual != want[i] {
							t.Fatalf("profile%d result%d: %x want%x", profile, i, got[i], want[i])
						}
					}
					for _, token := range got[:2] {
						if err := in.ReleaseGCRef(GCRef{token: token}); err != nil {
							t.Fatal(err)
						}
					}
					if err := in.CollectGC(); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

func gcCachedReadExtentModule() []byte {
	body := []byte{1, 1, 0x63, 0, 0x23, 0, 0x21, 0,
		0x20, 0, 0xfb, 4, 0, 0, 0x20, 0, 0xfb, 3, 0, 0,
		0x20, 0, 0xfb, 2, 0, 1, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 2, 0x78, 1, 0x7e, 1}, wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32, wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x7f, 0x42, 42, 0xfb, 0, 0, 0x24, 0, 0x0b}), append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestGCCachedReadPackedAndExtent(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcCachedReadExtentModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
		in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile}})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if _, err := in.Invoke("init"); err != nil {
			t.Fatal(err)
		}
		check := func() {
			t.Helper()
			got, err := in.Invoke("run")
			if err != nil || len(got) != 3 || got[0] != 255 || got[1] != 0xffffffff || got[2] != 42 {
				t.Fatalf("profile%d: %x, %v", profile, got, err)
			}
		}
		check()
		view := in.gc.NativeView()
		ref := uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type))
		handle := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(ref>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
		header := unsafe.Slice((*byte)(offHeapPtr(view.Spaces[handle[gc.NativeHandleSpaceOffset]].Base+uintptr(binary.LittleEndian.Uint32(handle)))), gc.HeaderSize)
		// The packed field fits, but the later i64 does not. The cache must
		// recheck the larger extent rather than accept the first certificate.
		size := binary.LittleEndian.Uint32(header[4:])
		binary.LittleEndian.PutUint32(header[4:], gc.PayloadOffset+1)
		_, err = in.Invoke("run")
		binary.LittleEndian.PutUint32(header[4:], size)
		var trap *TrapError
		if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
			t.Fatalf("profile%d larger extent: %v", profile, err)
		}
		check()
	}
}
