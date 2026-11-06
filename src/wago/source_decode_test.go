package wago

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestFusedSourceDecodeDiagnosticParity(t *testing.T) {
	cfg := NewRuntimeConfig().WithFunctionWorkers(1)
	makeModule := func(body []byte) []byte {
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
		)
	}
	cases := [][]byte{{}, []byte("not wasm"), benchAddOneModule(), makeModule(nil), makeModule([]byte{0xff, 0x0b}), makeModule([]byte{0x0b, 0x0b}), makeModule([]byte{0x41, 0}), makeModule([]byte{0x41, 0x80, 0x80, 0x80, 0x80, 0x80}), makeModule([]byte{0x41, 0, 0x04, 0x40, 0x05, 0x05, 0x0b, 0x0b}), makeModule([]byte{0xfc, 9, 0, 0x0b}), makeModule([]byte{0x41, 0, 0x0b})}
	badCustom := wasmtest.Section(0, append(wasmtest.Name("name"), 0xff))
	cases = append(cases, append(makeModule([]byte{0xff, 0x0b}), badCustom...))
	for i, data := range cases {
		var errors [2]string
		for mode, setting := range []string{"0", "1"} {
			t.Setenv("WAGO_FUSED_DECODE_VALIDATION", setting)
			compiled, err := Compile(cfg, bytes.Clone(data))
			if err != nil {
				errors[mode] = err.Error()
			}
			if compiled != nil {
				compiled.Close()
			}
		}
		if errors[0] != errors[1] {
			t.Fatalf("case %d diagnostic mismatch: separate=%q fused=%q", i, errors[0], errors[1])
		}
	}
	// Pending metadata is deliberately not a standalone syntax check.
	m, err := wasm.DecodeModuleForValidationWithFeatures(makeModule([]byte{0xff, 0x0b}), wasm.ValidationFeatures{})
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err == nil {
		t.Fatal("mandatory validation accepted malformed body")
	}
}
