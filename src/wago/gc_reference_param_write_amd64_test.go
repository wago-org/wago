//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func gcReferenceParamWriteModule(tee bool) []byte {
	reset := []byte{0xd0, 0x6d, 0x21, 0, 0x20, 0, 0x0b}
	if tee {
		reset = []byte{0xd0, 0x6d, 0x22, 0, 0x1a, 0x20, 0, 0x0b}
	}
	// Two call sites must return null. The caller's original local stays nonnull.
	run := []byte{1, 1, 0x6d, 0x41, 1, 0xfb, 28, 0x21, 0,
		0x20, 0, 0x10, 0, 0xd1,
		0x20, 0, 0x10, 0, 0xd1, 0x71,
		0x20, 0, 0xd1, 0x45, 0x71, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.EqRef}, []wasm.ValType{wasm.EqRef}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(reset), append(wasmtest.ULEB(uint32(len(run))), run...))),
	)
}
func TestGCForwardedReferenceParameterWrite(t *testing.T) {
	for _, inline := range []bool{false, true} {
		for _, tee := range []bool{false, true} {
			t.Run(fmt.Sprintf("inline%t/tee%t", inline, tee), func(t *testing.T) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithCompileWorkers(1).WithOptimization("inline", inline), gcReferenceParamWriteModule(tee))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c, InstantiateOptions{})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				for range 3 {
					got, err := in.Invoke("run")
					if err != nil || len(got) != 1 || got[0] != 1 {
						t.Fatalf("run=%v,%v; want [1]", got, err)
					}
				}
			})
		}
	}
}
