//go:build linux && amd64 && !tinygo

package wago

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcParameterLengthModule(final, nullable bool) []byte {
	array := []byte{0x5e, 0x7f, 1}
	if !final {
		array = append([]byte{0x50, 0}, array...)
	}
	ref := byte(0x63)
	if !nullable {
		ref = 0x64
	}
	types := [][]byte{array, {0x60, 1, ref, 0, 1, 0x7f}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), {0x60, 2, ref, 0, ref, 0, 1, 0x7f}}
	// Function0 reads its typed parameter. Function1 assigns another valid array
	// to that parameter before reading. Entry wrappers supply only Wasm objects.
	funcs := [][]byte{{1}, {4}, {2}, {3}, {3}, {3}}
	bodies := [][]byte{
		wasmtest.Code([]byte{0x20, 0, 0xfb, 0x0f, 0x0b}),
		wasmtest.Code([]byte{0x20, 1, 0x21, 0, 0x20, 0, 0xfb, 0x0f, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0xfb, 7, 0, 0x24, 0, 0x0b}),
		wasmtest.Code([]byte{0x23, 0, 0xd4, 0x10, 0, 0x0b}),
		wasmtest.Code([]byte{0x41, 3, 0xfb, 7, 0, 0x41, 9, 0xfb, 7, 0, 0x10, 1, 0x0b}),
	}
	if nullable {
		bodies = append(bodies, wasmtest.Code([]byte{0xd0, 0, 0x10, 0, 0x0b}))
	} else {
		bodies = append(bodies, wasmtest.Code([]byte{0xd0, 0, 0xd4, 0x10, 0, 0x0b}))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 2), wasmtest.ExportEntry("read", 0, 3), wasmtest.ExportEntry("assign", 0, 4), wasmtest.ExportEntry("null", 0, 5))),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func TestGCNativeFinalParameterArrayLength(t *testing.T) {
	for _, final := range []bool{false, true} {
		for _, nullable := range []bool{false, true} {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcParameterLengthModule(final, nullable))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
				in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, VerifyAfterCollect: true}})
				if err != nil {
					t.Fatal(err)
				}
				for _, n := range []uint64{0, 1, 31, 256, 4096} {
					if _, err := in.Invoke("init", n); err != nil {
						t.Fatal(err)
					}
					for round := 0; round < 2; round++ {
						got, err := in.Invoke("read")
						if err != nil || len(got) != 1 || got[0] != n {
							t.Fatalf("final%v nullable%v profile%d len%d: %v,%v", final, nullable, profile, n, got, err)
						}
						if err := in.CollectGC(); err != nil {
							t.Fatal(err)
						}
					}
				}
				got, err := in.Invoke("assign")
				if err != nil || len(got) != 1 || got[0] != 9 {
					t.Fatalf("assigned array: %v,%v", got, err)
				}
				_, err = in.Invoke("null")
				var trap *TrapError
				if !errors.As(err, &trap) || trap.Code != TrapNullReference {
					t.Fatalf("null: %v", err)
				}
				in.Close()
			}
		}
	}
}

func TestGCNativeParameterLengthSnapshotAfterAssignment(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, []byte{0x60, 2, 0x64, 0, 0x64, 0, 2, 0x7f, 0x7f}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x21, 0, 0xfb, 0x0f, 0x20, 0, 0xfb, 0x0f, 0x0b}),
			wasmtest.Code([]byte{0x41, 3, 0xfb, 7, 0, 0x41, 9, 0xfb, 7, 0, 0x10, 0, 0x0b}),
		)),
	)
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
	got, err := in.Invoke("run")
	if err != nil || len(got) != 2 || got[0] != 3 || got[1] != 9 {
		t.Fatalf("snapshot/current lengths: %v,%v; want[3 9]", got, err)
	}
}

func TestGCNativeParameterLengthTrapOrder(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, []byte{0x60, 2, 0x63, 0, 0x7f, 1, 0x7f}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 1, 0x20, 1, 0x6e, 0x20, 0, 0xfb, 0x0f, 0x6a, 0x0b}), wasmtest.Code([]byte{0xd0, 0, 0x20, 0, 0x10, 0, 0x0b}))),
	)
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
	for _, divisor := range []uint64{0, 1} {
		_, err := in.Invoke("run", divisor)
		var trap *TrapError
		want := TrapDivZero
		if divisor != 0 {
			want = TrapNullReference
		}
		if !errors.As(err, &trap) || trap.Code != want {
			t.Fatalf("divisor%d: %v; want%v", divisor, err, want)
		}
	}
}
