//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestIndexedAtomicWaitCollectorLocal(t *testing.T) {
	requireNativeTestFeatures(t, CoreFeatureGC)
	for _, address64 := range []bool{false, true} {
		for _, index := range []byte{0, 1} {
			for _, sub := range []byte{1, 2} {
				t.Run(fmt.Sprintf("addr64=%t/index=%d/op=%d", address64, index, sub), func(t *testing.T) {
					limits, constant, align, expected := byte(3), byte(0x41), byte(2), wasm.I32
					if address64 {
						limits, constant = 7, 0x42
					}
					if sub == 2 {
						align, expected = 3, wasm.I64
					}
					memories := [][]byte{{limits, 1, 2}, {1, 0, 1}}
					if index != 0 {
						memories[0], memories[1] = memories[1], memories[0]
					}
					body := []byte{2, 1, 0x63, 0, 1, 0x7f,
						0x41, 0xc9, 0, 0xfb, 0, 0, 0x21, 2,
						constant, 0, 0x20, 0, 0x20, 1, 0xfe, sub, align | 0x40, index, 0, 0x21, 3,
						0xfb, 1, 0, 0x1a, 0xfb, 1, 0, 0x1a, 0xfb, 1, 0, 0x1a,
						0x20, 2, 0xfb, 2, 0, 0, 0x20, 3, 0x0b}
					module := wasmtest.Module(
						wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{expected, wasm.I64}, []wasm.ValType{wasm.I32, wasm.I32}))),
						wasmtest.Section(3, wasmtest.Vec([]byte{1})),
						wasmtest.Section(5, wasmtest.Vec(memories...)),
						wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
						wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
					)
					c, err := indexedWaitConfig().Compile(module)
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					if admission := c.GCNativeRootAdmission(); !admission.Required || !admission.Exact {
						t.Fatalf("collector local lacks exact root admission: %+v", admission)
					}
					in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny,
						TinyHeapBytes: 64, TinyBlockBytes: 16, TinyCollectEveryAlloc: true,
						TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					for _, value := range []uint64{1, 0, 1, 0} {
						got, err := in.Invoke("run", value, 0)
						if err != nil || len(got) != 2 || got[0] != 73 || got[1] != 2-value {
							t.Fatalf("value%d then collect: %v,%v", value, got, err)
						}
					}
				})
			}
		}
	}
}
