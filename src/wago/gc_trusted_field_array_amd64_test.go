//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcTrustedFieldArrayModule() []byte {
	types := [][]byte{
		{0x4f, 0, 0x5e, 0x7f, 1},    // 0: final (array (mut i32))
		{0x4f, 0, 0x5f, 1, 0x6d, 1}, // 1: final (struct (field (mut eqref)))
		{0x4f, 0, 0x5f, 1, 0x7f, 1}, // 2: final (struct (field (mut i32)))
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	access := func(prefix []byte, nullable bool, index byte) []byte {
		cast := byte(22)
		if nullable {
			cast = 23
		}
		return wasmtest.Code(append(prefix,
			0xfb, 0, 1, // struct.new 1
			0xfb, 2, 1, 0, // struct.get 1 0
			0xfb, cast, 0, // ref.cast (ref [null] 0)
			0x41, index, // i32.const index
			0xfb, 11, 0, // array.get 0
			0x0b,
		))
	}
	bodies := [][]byte{
		access([]byte{0x41, 42, 0x41, 1, 0xfb, 6, 0}, false, 0), // array.new 0
		access([]byte{0xd0, 0x6d}, true, 0),                     // ref.null eq
		access([]byte{0x41, 1, 0xfb, 28}, false, 0),             // ref.i31
		access([]byte{0xfb, 1, 2}, false, 0),                    // struct.new_default 2
		access([]byte{0x41, 7, 0x41, 1, 0xfb, 6, 0}, false, 1),  // array.new 0
	}
	names := []string{"valid", "null", "i31", "wrong", "bounds"}
	funcs := make([][]byte, len(bodies))
	exports := make([][]byte, len(bodies))
	for i, name := range names {
		funcs[i] = wasmtest.ULEB(3)
		exports[i] = wasmtest.ExportEntry(name, byte(wasm.ExternFunc), uint32(i))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func TestGCTrustedFieldArrayGetRuntime(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcTrustedFieldArrayModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, cfg := range []GCConfig{
		{VerifyAfterCollect: true},
		{CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true},
		{Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, VerifyAfterCollect: true},
	} {
		in, err := Instantiate(c, InstantiateOptions{GC: cfg})
		if err != nil {
			t.Fatal(err)
		}
		got, err := in.Invoke("valid")
		if err != nil || len(got) != 1 || got[0] != 42 {
			in.Close()
			t.Fatalf("profile=%v valid=%v,%v want[42]", cfg.Profile, got, err)
		}
		for _, tc := range []struct {
			name string
			code TrapCode
		}{{"null", TrapNullReference}, {"i31", TrapCastFailure}, {"wrong", TrapCastFailure}, {"bounds", TrapBuiltin}} {
			_, err = in.Invoke(tc.name)
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != tc.code {
				in.Close()
				t.Fatalf("profile=%v %s=%v want trap%d", cfg.Profile, tc.name, err, tc.code)
			}
		}
		in.Close()
	}
}

func gcTrustedStoredCastModule() []byte {
	types := [][]byte{
		{0x4f, 0, 0x5e, 0x7f, 1},    // 0: final (array (mut i32))
		{0x4f, 0, 0x5f, 1, 0x6d, 1}, // 1: final (struct (field (mut eqref)))
		{0x4f, 0, 0x5f, 1, 0x7f, 1}, // 2: final (struct (field (mut i32)))
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	cast := func(value []byte) []byte {
		return wasmtest.Code(append(value,
			0xfb, 0, 1, // struct.new 1
			0xfb, 2, 1, 0, // struct.get 1 0
			0xfb, 22, 0, // ref.cast (ref 0)
			0x1a,     // drop
			0x41, 42, // i32.const 42
			0x0b,
		))
	}
	bodies := [][]byte{
		cast([]byte{0x41, 7, 0x41, 1, 0xfb, 6, 0}), // array.new 0
		cast([]byte{0xd0, 0x6d}),                   // ref.null eq
		cast([]byte{0x41, 1, 0xfb, 28}),            // ref.i31
		cast([]byte{0xfb, 1, 2}),                   // struct.new_default 2
	}
	names := []string{"valid", "null", "i31", "wrong"}
	funcs := make([][]byte, len(bodies))
	exports := make([][]byte, len(bodies))
	for i, name := range names {
		funcs[i] = wasmtest.ULEB(3)
		exports[i] = wasmtest.ExportEntry(name, byte(wasm.ExternFunc), uint32(i))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func TestGCTrustedStoredCastSharedRuntime(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcTrustedStoredCastModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, cfg := range []GCConfig{
		{VerifyAfterCollect: true},
		{CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true},
		{Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true},
	} {
		in, err := Instantiate(c, InstantiateOptions{GC: cfg})
		if err != nil {
			t.Fatal(err)
		}
		got, err := in.Invoke("valid")
		if err != nil || len(got) != 1 || got[0] != 42 {
			in.Close()
			t.Fatalf("profile=%v valid=%v,%v want[42]", cfg.Profile, got, err)
		}
		for _, name := range []string{"null", "i31", "wrong"} {
			_, err = in.Invoke(name)
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
				in.Close()
				t.Fatalf("profile=%v %s=%v want trap%d", cfg.Profile, name, err, TrapCastFailure)
			}
		}
		if err := in.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func gcTrustedGuardFieldArrayModule() []byte {
	guard := wasmtest.Code([]byte{
		0x20, 1, // local.get index
		0x20, 0, // local.get object
		0xfb, 22, 1, // ref.cast (ref 1)
		0xfb, 2, 1, 1, // struct.get 1 1
		0x4f,       // i32.ge_u
		0x04, 0x7f, // if (result i32)
		0x00,    // unreachable
		0x05,    // else
		0x20, 0, // local.get object
		0xfb, 22, 1, // ref.cast (ref 1)
		0xfb, 2, 1, 0, // struct.get 1 0
		0xfb, 22, 0, // ref.cast (ref 0)
		0x20, 1, // local.get index
		0xfb, 11, 0, // array.get 0
		0x0b, // end if
		0x0b, // end function
	})
	wrapper := wasmtest.Code([]byte{
		0x41, 42, // i32.const 42
		0x41, 1, // i32.const array length
		0xfb, 6, 0, // array.new 0
		0x41, 1, // i32.const field 1 limit
		0xfb, 0, 1, // struct.new 1
		0x41, 0, // i32.const index
		0x10, 0, // call guard
		0x0b,
	})
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x4f, 0, 0x5e, 0x7f, 1},             // final (array (mut i32))
			[]byte{0x4f, 0, 0x5f, 2, 0x6d, 1, 0x7f, 1}, // final (struct (field (mut eqref)) (field (mut i32)))
			[]byte{0x60, 2, 0x64, 1, 0x7f, 1, 0x7f},    // guard
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{3})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(guard, wrapper)),
	)
}

func TestGCTrustedTrappingGuardFieldArrayGetRuntime(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcTrustedGuardFieldArrayModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, cfg := range []GCConfig{
		{VerifyAfterCollect: true},
		{CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true},
		{Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, VerifyAfterCollect: true},
	} {
		in, err := Instantiate(c, InstantiateOptions{GC: cfg})
		if err != nil {
			t.Fatal(err)
		}
		got, err := in.Invoke("run")
		in.Close()
		if err != nil || len(got) != 1 || got[0] != 42 {
			t.Fatalf("profile=%v run=%v,%v want[42]", cfg.Profile, got, err)
		}
	}
}
