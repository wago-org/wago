package wasm_test

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestMemoryWireGrammarUsesFeatures(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"load explicit zero", []byte{0x41, 0, 0x28, 0x42, 0, 0, 0x1a}},
		{"size padded zero", []byte{0x3f, 0x80, 0, 0x1a}},
		{"grow padded zero", []byte{0x41, 0, 0x40, 0x80, 0, 0x1a}},
		{"fill padded zero", []byte{0x41, 0, 0x41, 0, 0x41, 0, 0xfc, 11, 0x80, 0}},
		{"copy padded zero", []byte{0x41, 0, 0x41, 0, 0x41, 0, 0xfc, 10, 0x80, 0, 0}},
		{"init padded zero", []byte{0x41, 0, 0x41, 0, 0x41, 0, 0xfc, 8, 0, 0x80, 0}},
		{"SIMD explicit zero", []byte{0x41, 0, 0xfd, 0, 0x40, 0, 0, 0x1a}},
		{"atomic explicit zero", []byte{0x41, 0, 0xfe, 0x10, 0x42, 0, 0, 0x1a}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
				wasmtest.Section(5, wasmtest.Vec([]byte{3, 1, 1})),
				wasmtest.Section(12, []byte{1}),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(append(tc.body, 0x0b)))),
				wasmtest.Section(11, wasmtest.Vec([]byte{1, 0})),
			)
			for _, multi := range []bool{false, true} {
				features := wasm.ValidationFeatures{MultiMemory: multi}
				err := wasm.ValidateByteBackedModuleWithFeatures(source, features)
				if (err == nil) != multi {
					t.Fatalf("byte-backed multi=%v: %v", multi, err)
				}
				module, err := wasm.DecodeModuleWithFeatures(source, features)
				if err == nil {
					err = wasm.ValidateModuleWithFeatures(module, features)
				}
				if (err == nil) != multi {
					t.Fatalf("decode/validate multi=%v: %v", multi, err)
				}
				module, err = wasm.DecodeModule(source)
				if err != nil {
					t.Fatal(err)
				}
				err = wasm.ValidateModuleWithFeatures(module, features)
				if (err == nil) != multi {
					t.Fatalf("syntax-only then validate multi=%v: %v", multi, err)
				}
			}
		})
	}
}
