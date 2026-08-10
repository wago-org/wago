//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestGCBoundedLoadForwardingExecutes(t *testing.T) {
	tests := []struct {
		name      string
		composite []byte
		body      []byte
		arg       uint64
	}{
		{
			name:      "immutable-struct-get",
			composite: []byte{0x5f, 0x01, 0x7f, 0x00},
			body: []byte{
				0x02, 0x01, 0x63, 0x00, 0x01, 0x7f,
				0x20, 0x00, 0xfb, 0x00, 0x00, 0x21, 0x01,
				0x20, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x21, 0x02,
				0x20, 0x01, 0xfb, 0x02, 0x00, 0x00,
				0x0b,
			},
			arg: 73,
		},
		{
			name:      "array-len",
			composite: []byte{0x5e, 0x7f, 0x01},
			body: []byte{
				0x02, 0x01, 0x63, 0x00, 0x01, 0x7f,
				0x20, 0x00, 0xfb, 0x07, 0x00, 0x21, 0x01,
				0x20, 0x01, 0xfb, 0x0f, 0x21, 0x02,
				0x20, 0x01, 0xfb, 0x0f,
				0x0b,
			},
			arg: 37,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(tc.composite, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
				wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(tc.body))), tc.body...))),
			)
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			for _, profile := range []GCConfig{
				{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true},
				{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true},
			} {
				instance, err := Instantiate(compiled, InstantiateOptions{GC: profile})
				if err != nil {
					t.Fatal(err)
				}
				got, err := instance.Invoke("run", tc.arg)
				_ = instance.Close()
				if err != nil || len(got) != 1 || got[0] != tc.arg {
					t.Fatalf("run(%d) = %v, %v", tc.arg, got, err)
				}
			}
		})
	}
}

func TestGCExactReferenceFactClearsOnLocalSet(t *testing.T) {
	body := []byte{
		0x01, 0x01, 0x63, 0x00, // one (ref null 0) local
		0xfb, 0x01, 0x00, 0x21, 0x00, // exact non-null constructor -> local 0
		0xd0, 0x00, 0x21, 0x00, // overwrite local 0 with null
		0x20, 0x00, 0xfb, 0x16, 0x00, // non-null ref.cast must still trap
		0x1a,
		0x41, 0x07,
		0x0b,
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run"); err == nil {
		t.Fatalf("run = %v, want null cast trap", got)
	}
}
