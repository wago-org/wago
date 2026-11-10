//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestPrimitiveAggregateDeclarationsRequirePrimitiveExecution(t *testing.T) {
	for _, test := range []struct {
		name            string
		signature, body []byte
		admitted        bool
	}{
		{"inert", []byte{0x60, 0, 1, 0x7f}, []byte{0x41, 42, 0x0b}, true},
		{"struct-allocation", []byte{0x60, 0, 1, 0x7f}, []byte{0xfb, 1, 1, 0x1a, 0x41, 42, 0x0b}, false},
		{"collector-parameter", []byte{0x60, 1, 0x63, 1, 1, 0x7f}, []byte{0x41, 42, 0x0b}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			types := append([]byte{1, 0x4e, 2}, test.signature...)
			types = append(types, 0x5f, 1, 0x63, 0, 0)
			source := wasmtest.Module(wasmtest.Section(1, types), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(test.body))))
			decoded, err := wasm.DecodeModuleByteBackedWithLimits(source, wasm.ValidationFeatures{}, wasm.DecodeLimits{MaxTypes: MaxFunctions, MaxMetadataBytes: MaxModuleBytes})
			if err != nil {
				t.Fatal(err)
			}
			if err := wasm.ValidateModule(decoded.Module); err != nil {
				t.Fatal("invalid fixture", err)
			}
			if got := AdmitPrimitiveFunctionGraphModule(decoded.Module); got != test.admitted {
				t.Fatal("primitive execution admission", got)
			}
		})
	}
}

func TestPrimitiveTypedNullInitializer(t *testing.T) {
	m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{
		{Final: true, Comp: wasm.CompType{Kind: wasm.CompFunc}},
		{Final: true, Comp: wasm.CompType{Kind: wasm.CompStruct}},
	}}}}
	for _, test := range []struct {
		name string
		body []byte
		want bool
	}{
		{"indexed-function", []byte{0xd0, 0, 0x0b}, true},
		{"abstract-function", []byte{0xd0, 0x70, 0x0b}, true},
		{"abstract-external", []byte{0xd0, 0x6f, 0x0b}, true},
		{"bottom-function", []byte{0xd0, 0x73, 0x0b}, true},
		{"bottom-external", []byte{0xd0, 0x72, 0x0b}, true},
		{"aggregate", []byte{0xd0, 1, 0x0b}, false},
		{"unknown-index", []byte{0xd0, 2, 0x0b}, false},
		{"overflow-index", []byte{0xd0, 0xff, 0xff, 0xff, 0xff, 15, 0x0b}, false},
		{"abstract-aggregate", []byte{0xd0, 0x6b, 0x0b}, false},
		{"trailing", []byte{0xd0, 0, 0x0b, 0}, false},
		{"missing-end", []byte{0xd0, 0}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, got := primitiveReferenceNullInitializer(wasm.Expr{BodyBytes: test.body}, m)
			if got != test.want {
				t.Fatal("null initializer admission", got)
			}
		})
	}
}
