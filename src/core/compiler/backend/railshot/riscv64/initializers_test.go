//go:build linux && !tinygo

package riscv64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestProductionImportedGlobalInitializers(t *testing.T) {
	types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
	var imports, globals [][]byte
	for i, typ := range types {
		imports = append(imports, wasmtest.GlobalImportEntry("env", "g", typ, false))
		globals = append(globals, wasmtest.GlobalEntry(typ, true, []byte{0x23, byte(i), 0x0b}))
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, types))),
		wasmtest.Section(2, wasmtest.Vec(imports...)),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(5, []byte{1, 0, 1}),
		wasmtest.Section(6, wasmtest.Vec(globals...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 4, 0x23, 5, 0x23, 6, 0x23, 7, 0x0b}))),
		wasmtest.Section(11, []byte{1, 0, 0x23, 0, 0x0b, 1, 42}),
	)
	for _, heap := range []bool{false, true} {
		image, err := CompileProduction(data, ProductionOptions{DeferCodeMapping: heap})
		if err != nil {
			t.Fatal(err)
		}
		if err := image.CodeImage.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if image, err := Compile(data); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("isolated initializer admission widened", image, err)
	}
}

func TestImportedGlobalInitializerExactAdmission(t *testing.T) {
	imports := []globalSpec{{typ: wasm.I32}, {typ: wasm.I64}, {typ: wasm.F32}, {typ: wasm.F64}, {typ: wasm.I32, mutable: true}}
	for _, test := range []struct {
		name string
		typ  wasm.ValType
		body []byte
	}{
		{"same-width-f32", wasm.I32, []byte{0x23, 2, 0x0b}},
		{"same-width-f64", wasm.I64, []byte{0x23, 3, 0x0b}},
		{"mutable", wasm.I32, []byte{0x23, 4, 0x0b}},
		{"defined-index", wasm.I32, []byte{0x23, 5, 0x0b}},
		{"extended", wasm.I32, []byte{0x23, 0, 0x41, 1, 0x6a, 0x0b}},
		{"trailing", wasm.I32, []byte{0x23, 0, 0x0b, 0x0b}},
		{"missing-end", wasm.I32, []byte{0x23, 0}},
		{"truncated-index", wasm.I32, []byte{0x23, 0x80}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := admittedScalarConst(wasm.Expr{BodyBytes: test.body}, test.typ, true, imports); !errors.Is(err, ErrUnsupported) {
				t.Fatal("initializer escaped exact admission", err)
			}
		})
	}
}

func TestProductionDefinedGlobalInitializerReferenceStaysGated(t *testing.T) {
	data := modifySection(binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x23, 1, 0x0b}}), 7, 6, []byte{2, 0x7f, 0, 0x41, 7, 0x0b, 0x7f, 0, 0x23, 0, 0x0b}, false)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{ExtendedConstGlobals: true}, 1, wasm.ValidationLimits{}); err != nil {
		t.Fatal("fixture must be valid before RV64 policy", err)
	}
	if _, err := admitStateMode(m, true); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	for _, heap := range []bool{false, true} {
		if image, err := CompileProduction(data, ProductionOptions{DeferCodeMapping: heap}); image != nil || err == nil {
			t.Fatal(image, err)
		}
	}
}
