//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcLocalTypeFactRuntimeModule() []byte {
	withAnyrefLocal := func(instructions []byte) []byte {
		body := append([]byte{0x01, 0x01, 0x6e}, instructions...) // one anyref local
		return append(wasmtest.ULEB(uint32(len(body))), body...)
	}
	types := wasmtest.Vec(
		[]byte{0x50, 0, 0x5f, 1, 0x7f, 1},    // 0: open base struct
		[]byte{0x50, 1, 0, 0x5f, 1, 0x7f, 1}, // 1: open child struct <: 0
		[]byte{0x4f, 0, 0x5f, 1, 0x7f, 1},    // 2: unrelated final struct
		[]byte{0x60, 0, 1, 0x64, 1},          // 3: () -> (ref 1)
		wasmtest.FuncType(nil, []wasm.ValType{wasm.AnyRef}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	)
	producer := wasmtest.Code([]byte{0x41, 42, 0xfb, 0, 1, 0x0b})
	unknown := wasmtest.Code([]byte{0x41, 7, 0xfb, 0, 2, 0x0b})
	success := withAnyrefLocal([]byte{
		0x10, 0, 0x21, 0, // local 0 = producer()
		0x20, 0,
		0xfb, 22, 0, // ref.cast (ref 0), proved by the local fact
		0xfb, 2, 0, 0, // struct.get 0 0
		0x0b,
	})
	trap := withAnyrefLocal([]byte{
		0x10, 0, 0x21, 0, // first store has a precise child fact
		0x10, 1, 0x21, 0, // unknown anyref result must invalidate that fact
		0x20, 0,
		0xfb, 22, 0, // unrelated object must still fail this cast
		0xfb, 2, 0, 0,
		0x0b,
	})
	return wasmtest.Module(
		wasmtest.Section(1, types),
		wasmtest.Section(3, wasmtest.Vec([]byte{3}, []byte{4}, []byte{5}, []byte{5})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("success", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("trap", byte(wasm.ExternFunc), 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(producer, unknown, success, trap)),
	)
}

func TestGCLocalTypeFactRuntime(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcLocalTypeFactRuntimeModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	for _, tc := range []struct {
		name string
		gc   GCConfig
	}{
		{name: "normal", gc: GCConfig{VerifyAfterCollect: true}},
		{name: "collect-every-allocation", gc: GCConfig{CollectEveryAlloc: true, ForceMajorEveryMinor: true, StressNurseryBytes: 128, VerifyAfterCollect: true}},
		{name: "tiny", gc: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := Instantiate(compiled, InstantiateOptions{GC: tc.gc})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			got, err := in.Invoke("success")
			if err != nil || len(got) != 1 || got[0] != 42 {
				t.Fatalf("success = %v, %v; want [42]", got, err)
			}
			_, err = in.Invoke("trap")
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
				t.Fatalf("unknown overwrite = %v; want cast failure", err)
			}
		})
	}
}
