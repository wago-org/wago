//go:build linux && amd64 && !tinygo

package wago

import (
	"errors"
	"fmt"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcOpenParameterLengthModule(storage byte, nullable, child bool) []byte {
	root := []byte{0x50, 0, 0x5e, storage, 1}
	ref := byte(0x63)
	if !nullable {
		ref = 0x64
	}
	createdType := byte(1)
	if child {
		createdType = 2
	}
	types := [][]byte{
		root, root, {0x4f, 1, 1, 0x5e, storage, 1},
		{0x60, 1, ref, 1, 1, 0x7f},
		{0x60, 2, ref, 1, ref, 1, 1, 0x7f},
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		{0x60, 2, ref, 1, ref, 1, 2, 0x7f, 0x7f},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32}),
	}
	read := []byte{0x23, 0}
	null := []byte{0xd0, 1}
	if !nullable {
		read = append(read, 0xd4)
		null = append(null, 0xd4)
	}
	read = append(read, 0x10, 0, 0x0b)
	null = append(null, 0x10, 0, 0x0b)
	objects := []byte{0x41, 3, 0xfb, 7, 1, 0x41, 9, 0xfb, 7, 2}
	assign := append(append([]byte(nil), objects...), 0x10, 1, 0x0b)
	snapshot := append(append([]byte(nil), objects...), 0x10, 6, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec([]byte{3}, []byte{4}, []byte{5}, []byte{6}, []byte{6}, []byte{6}, []byte{7}, []byte{8})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 1, 1, 0xd0, 1, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 2), wasmtest.ExportEntry("read", 0, 3), wasmtest.ExportEntry("assign", 0, 4), wasmtest.ExportEntry("null", 0, 5), wasmtest.ExportEntry("snapshot", 0, 7))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xfb, 0x0f, 0x0b}),
			wasmtest.Code([]byte{0x20, 1, 0x21, 0, 0x20, 0, 0xfb, 0x0f, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xfb, 7, createdType, 0x24, 0, 0x0b}),
			wasmtest.Code(read), wasmtest.Code(assign), wasmtest.Code(null),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x21, 0, 0xfb, 0x0f, 0x20, 0, 0xfb, 0x0f, 0x0b}),
			wasmtest.Code(snapshot),
		)),
	)
}

func TestGCNativeOpenParameterArrayLength(t *testing.T) {
	for _, elem := range []struct {
		name    string
		storage byte
	}{{"i32", 0x7f}, {"i8", 0x78}, {"v128", 0x7b}, {"eqref", 0x6d}} {
		for _, nullable := range []bool{false, true} {
			for _, child := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/nullable%v/child%v", elem.name, nullable, child), func(t *testing.T) {
					c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcOpenParameterLengthModule(elem.storage, nullable, child))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
						// The largest vector array exceeds the default tiny heap.
						in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile, TinyHeapBytes: 256 << 10, VerifyAfterCollect: true}})
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						localTypes := unsafe.Slice((*uint32)(offHeapPtr(in.gcNativeView.LocalTypes)), in.gcNativeView.LocalTypeCount)
						if len(localTypes) <= 2 || localTypes[1] == 1 || localTypes[2] == 2 {
							t.Fatal("fixture did not remap array types")
						}
						for _, n := range []uint64{0, 1, 31, 256, 4096} {
							if _, err := in.Invoke("init", n); err != nil {
								t.Fatal(err)
							}
							for round := 0; round < 2; round++ {
								got, err := in.Invoke("read")
								if err != nil || len(got) != 1 || got[0] != n {
									t.Fatalf("profile%d length%d: %v,%v", profile, n, got, err)
								}
								if err := in.CollectGC(); err != nil {
									t.Fatal(err)
								}
							}
						}
						got, err := in.Invoke("assign")
						if err != nil || len(got) != 1 || got[0] != 9 {
							t.Fatalf("assigned length: %v,%v", got, err)
						}
						got, err = in.Invoke("snapshot")
						if err != nil || len(got) != 2 || got[0] != 3 || got[1] != 9 {
							t.Fatalf("snapshot/current lengths: %v,%v", got, err)
						}
						_, err = in.Invoke("null")
						var trap *TrapError
						if !errors.As(err, &trap) || trap.Code != TrapNullReference {
							t.Fatalf("null: %v", err)
						}
					}
				})
			}
		}
	}
}

func TestGCNativeOpenParameterLengthTrapOrder(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x50, 0, 0x5e, 0x7f, 1}, []byte{0x60, 2, 0x63, 0, 0x7f, 1, 0x7f}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
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
