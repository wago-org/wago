//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func directReferenceABILoopModule(referenceResult bool) []byte {
	ref := wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false))
	result := wasm.I32
	leaf := []byte{0x20, 0}
	if referenceResult {
		result = ref
	} else {
		leaf = append(leaf, 0xfb, 2, 0, 0)
	}
	leaf = append(leaf, 0x0b)
	loop := []byte{2, 1, 0x63, 0, 2, 0x7f,
		0x41, 7, 0xfb, 0, 0, 0x21, 1,
		0x02, 0x40, 0x03, 0x40, 0x20, 2, 0x20, 0, 0x4f, 0x0d, 1,
		0x20, 3, 0x20, 1, 0x10, 0}
	if referenceResult {
		loop = append(loop, 0xfb, 2, 0, 0)
	}
	loop = append(loop, 0x6a, 0x21, 3,
		0x20, 2, 0x41, 1, 0x6a, 0x21, 2, 0x0c, 0, 0x0b, 0x0b, 0x20, 3, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, directReferenceABIFuncType([]wasm.ValType{ref}, []wasm.ValType{result}), directReferenceABIFuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("loop", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(leaf), append(wasmtest.ULEB(uint32(len(loop))), loop...))),
	)
}

func BenchmarkDirectReferenceABI(b *testing.B) {
	for _, referenceResult := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			b.Run(fmt.Sprintf("referenceResult%t/enabled%t", referenceResult, enabled), func(b *testing.B) {
				cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false).WithOptimization("direct-reference-abi", enabled)
				c, err := Compile(cfg, directReferenceABILoopModule(referenceResult))
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				if status := c.GCNativeRootAdmission(); !status.Required || !status.Exact {
					b.Fatalf("benchmark lost exact roots: %+v", status)
				}
				in, err := Instantiate(c)
				if err != nil {
					b.Fatal(err)
				}
				defer in.Close()
				invoke := func() {
					got, err := in.Invoke("loop", 8192)
					if err != nil || len(got) != 1 || got[0] != 7*8192 {
						b.Fatalf("loop=%v,%v, want [%d]", got, err, 7*8192)
					}
				}
				invoke()
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					invoke()
				}
			})
		}
	}
}
