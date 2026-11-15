//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCNativeReferenceCastWrapper(t *testing.T) {
	requireCompleteCore3Backend(t)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 1, 0x7f, 0},
			[]byte{0x5f, 0},
			wasmtest.FuncType([]wasm.ValType{wasm.EqRef}, []wasm.ValType{wasm.EqRef}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{2}, []byte{2}, []byte{3}, []byte{3}, []byte{3})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("valid", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("null", byte(wasm.ExternFunc), 3),
			wasmtest.ExportEntry("wrong", byte(wasm.ExternFunc), 4),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xfb, 22, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x0b}),
			wasmtest.Code([]byte{0x41, 37, 0xfb, 0, 0, 0x10, 1, 0xfb, 22, 0, 0xfb, 2, 0, 0, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0x6d, 0x10, 1, 0xfb, 22, 0, 0xfb, 2, 0, 0, 0x0b}),
			wasmtest.Code([]byte{0xfb, 0, 1, 0x10, 1, 0xfb, 22, 0, 0xfb, 2, 0, 0, 0x0b}),
		)),
	)
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, cfg := range []GCConfig{{VerifyAfterCollect: true}, {Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}} {
		in, err := Instantiate(c, InstantiateOptions{GC: cfg})
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer in.Close()
			for i := 0; i < 3; i++ {
				got, err := in.Invoke("valid")
				if err != nil || len(got) != 1 || got[0] != 37 {
					t.Fatalf("valid=%v, %v; want37", got, err)
				}
				for _, name := range []string{"null", "wrong"} {
					_, err := in.Invoke(name)
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
						t.Fatalf("%s=%v; want cast failure", name, err)
					}
					if len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 1 || trap.Frames[0].HasProgramCounter {
						t.Fatalf("%s changed wrapper trap source: %+v", name, trap.Frames)
					}
				}
				if err := in.CollectGC(); err != nil {
					t.Fatal(err)
				}
			}
		}()
	}
}
