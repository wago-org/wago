//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func customOutputTailModule(name string, carrier byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, carrier})),
		wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("test:custom", name, 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x12, 0, 0x0b}))),
	)
}

func TestCustomImportedTailRejectsCustomOutput(t *testing.T) {
	if !customCodegenAvailable() {
		t.Skip("custom producer needs optional native CPU features")
	}
	for _, regABI := range []bool{false, true} {
		for i, carrier := range []byte{0x7f, 0x7e, 0x7d, 0x7c, 0x7b, 0x70, 0x6f} {
			t.Run(fmt.Sprintf("carrier%x/regABI%t", carrier, regABI), func(t *testing.T) {
				rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("reg-abi", regABI)))
				t.Cleanup(func() { rt.Close() })
				if err := rt.Use(customCarrierExtension{}); err != nil {
					t.Fatal(err)
				}
				mod, err := rt.Compile(customOutputTailModule(fmt.Sprintf("test.value.%d", i), carrier))
				if mod != nil {
					t.Cleanup(func() { mod.Close() })
				}
				if err == nil || !strings.Contains(err.Error(), "return_call: custom output cannot cross a function boundary") {
					t.Fatalf("custom output tail compilation = %v, want explicit boundary rejection", err)
				}
			})
		}
	}
}
