//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCAbstractArrayCastLenIncrementalRuntimeRoots(t *testing.T) {
	for _, cfg := range []GCConfig{{}, {CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}, {Profile: GCProfileTiny, TinyHeapBytes: 1 << 20, VerifyAfterCollect: true}} {
		func() {
			rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
			defer rt.Close()
			array := []byte{0x5e, 0x7f, 1}
			seed, err := rt.Compile(wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(array, wasmtest.FuncType(nil, nil))),
				wasmtest.Section(3, wasmtest.Vec([]byte{1})),
				wasmtest.Section(6, wasmtest.Vec([]byte{0x6e, 1, 0xd0, 0x6e, 0x0b})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("anchor", byte(wasm.ExternGlobal), 0), wasmtest.ExportEntry("init", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 1, 0xfb, 7, 0, 0x24, 0, 0x0b}))),
			))
			if err != nil {
				t.Fatal(err)
			}
			defer seed.Close()
			first, err := rt.Instantiate(context.Background(), seed, WithGC(cfg))
			if err != nil {
				t.Fatal(err)
			}
			defer first.Close()
			anchor, err := first.ExportedGlobalObject("anchor")
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := rt.Compile(wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(array, []byte{0x5f, 2, 0x7e, 1, 0x7e, 1}, []byte{0x5e, 0x7e, 1},
					wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "anchor", wasm.AnyRef, true))),
				wasmtest.Section(3, wasmtest.Vec([]byte{3}, []byte{4}, []byte{5}, []byte{5})),
				wasmtest.Section(6, wasmtest.Vec([]byte{0x6e, 1, 0xd0, 0x6e, 0x0b})),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("structure", 0, 0), wasmtest.ExportEntry("array", 0, 1), wasmtest.ExportEntry("length", 0, 2), wasmtest.ExportEntry("nullable-length", 0, 3))),
				wasmtest.Section(10, wasmtest.Vec(
					wasmtest.Code([]byte{0x42, 7, 0x42, 9, 0xfb, 0, 1, 0x24, 1, 0x0b}),
					wasmtest.Code([]byte{0x42, 11, 0x20, 0, 0xfb, 6, 2, 0x24, 1, 0x0b}),
					wasmtest.Code([]byte{0x23, 1, 0xfb, 22, 0x6a, 0xfb, 15, 0x0b}),
					wasmtest.Code([]byte{0x23, 1, 0xfb, 23, 0x6a, 0xfb, 15, 0x0b}))),
			))
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			before := first.gc.NativeView().SubtypeIntervalCount
			in, err := rt.Instantiate(context.Background(), consumer, WithGC(cfg), WithImports(testImports("env.anchor", anchor)))
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			structureType, structureOK := in.gcTypeMap.domain(1)
			arrayType, arrayOK := in.gcTypeMap.domain(2)
			if in.gc != first.gc || !structureOK || !arrayOK || uint32(structureType) < before || uint32(arrayType) < before {
				t.Fatal("fixture did not append its struct and array roots to the shared collector")
			}
			for _, encoding := range []uint32{0, gc.NativeSubtypeEncodingKindBit} {
				if _, err := in.Invoke("structure"); err != nil {
					t.Fatal(err)
				}
				in.gc.NativeView().SubtypeEncoding = encoding
				for _, name := range []string{"length", "nullable-length"} {
					_, err := in.Invoke(name)
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
						t.Fatalf("encoding %d %s(struct)=%v; want cast failure", encoding, name, err)
					}
				}
				for _, length := range []uint64{0, 1, 17} {
					if _, err := in.Invoke("array", length); err != nil {
						t.Fatal(err)
					}
					in.gc.NativeView().SubtypeEncoding = encoding
					for _, name := range []string{"length", "nullable-length"} {
						got, err := in.Invoke(name)
						if err != nil || len(got) != 1 || got[0] != length {
							t.Fatalf("encoding %d %s(array)=%v,%v; want %d", encoding, name, got, err, length)
						}
					}
				}
			}
		}()
	}
}
