//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"

	"testing"
)

func TestRV64FunctionCarrierSourceProof(t *testing.T) {
	inputs := map[string][]byte{
		"parameter across allocation": wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 1, 0x7f}, []byte{0x5f, 1, 0x7f, 0}, []byte{0x60, 1, 0x63, 0, 1, 0x7f})), wasmtest.Section(3, []byte{1, 2}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x41, 1, 0xfb, 0, 1, 0x1a, 0x14, 0, 0x0b})))),
		"mixed declaration":           wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), []byte{0x60, 0x0c, 0x70, 0x6f, 0x64, 0x70, 0x64, 0x6e, 0x64, 0, 0x64, 0, 0x64, 0, 0x64, 0, 0x70, 0x6f, 0x63, 0, 0x63, 0, 0})), wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b})))),
	}
	for name, source := range inputs {
		t.Run(name, func(t *testing.T) {
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			if err := wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			t.Logf("wire=%t decoded=%t public=%t carriers=%t storage=%t EHsource=%t", admitRV64GCRootedScalarWire(source), admitRV64GCRootedScalarModule(m), AdmitGCRootedScalarSource(source), wasm.ModuleFunctionReferenceCarriers(m), wasm.ModuleFunctionReferenceStorage(m), AdmitExceptionHandlingSource(source))
			if !admitRV64GCRootedScalarModule(m) {
				t.Fatal("valid mixed function carrier module did not enter rooted proof")
			}
		})
	}
}
