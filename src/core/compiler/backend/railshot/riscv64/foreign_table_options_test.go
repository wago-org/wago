//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func foreignTableOptionFixture(vector bool) []byte {
	if !vector {
		return ownedTableFixture()
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.I32}, []wasm.ValType{wasm.V128}),
			wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, []byte{2, 0, 1}),
		wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x11, 1, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x0b}))),
	)
}

func TestProductionForeignTableOptionPropagation(t *testing.T) {
	for _, vector := range []bool{false, true} {
		m, err := wasm.DecodeModule(foreignTableOptionFixture(vector))
		if err != nil {
			t.Fatal(err)
		}
		ordinary, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
		if err != nil {
			t.Fatal(err)
		}
		foreign, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true})
		if err != nil {
			t.Fatal(err)
		}
		key, ok := m.StructuralTypeKeyChecked(1)
		if !ok {
			t.Fatal("target signature")
		}
		cells := 1
		if vector {
			cells = 2
		}
		// The independent bridge generator has the exact signature, caller index
		// and source PC. Check the public option rather than hidden emissionMode.
		bridge, err := nativeTableThunkAt(key, cells, cells, 0, 5)
		if err != nil || !bytes.Contains(foreign.Code, bridge) || bytes.Contains(ordinary.Code, bridge) || bytes.Equal(foreign.Code, ordinary.Code) {
			t.Fatal("public option lost genuine framed bridge", vector, err)
		}
		pressure := 0
		image, err := CompileModuleWith(m, CompileOptions{ForeignTableCalls: true, MemoryPressure: func() { pressure++ }})
		if image != nil || err == nil || pressure != 0 || !strings.Contains(err.Error(), "foreign table calls require") {
			t.Fatal("noncooperative foreign option reached staging", vector, err, pressure)
		}
	}
	// A normally admitted table-free source isolates this new gate from the
	// older isolated-table and interruptible-mode rejection paths.
	source := binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x41, 7, 0x0b}})
	if _, err := compileOptions(source, scalarRegisters[:], CompileOptions{}); err != nil {
		t.Fatal("isolated control fixture", err)
	}
	if image, err := compileOptions(source, scalarRegisters[:], CompileOptions{ForeignTableCalls: true}); image != nil || err == nil || !strings.Contains(err.Error(), "foreign table calls require the production cooperative runtime ABI") {
		t.Fatal("isolated foreign option admitted", err)
	}
}

func TestProductionForeignTableOptionTableFreeByteIdentity(t *testing.T) {
	for _, source := range [][]byte{
		binaryModule(fixtureFunc{result: 0x7f, body: []byte{0x41, 7, 0x0b}}),
		binaryModule(fixtureFunc{params: []byte{0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x0b}}),
	} {
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		plain, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
		if err != nil {
			t.Fatal(err)
		}
		foreign, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true})
		if err != nil || !bytes.Equal(plain.Code, foreign.Code) {
			t.Fatal("table-free code changed", err)
		}
	}
}

func TestProductionForeignTableAggregateBoundBeforeStaging(t *testing.T) {
	for _, vector := range []bool{false, true} {
		var values []wasm.ValType
		leaf := []byte{0x0b}
		if vector {
			values = []wasm.ValType{wasm.V128}
			leaf = []byte{0x20, 0, 0x0b}
		}
		var body []byte
		for range 128 {
			if vector {
				body = append(body, 0xfd, 12)
				body = append(body, make([]byte, 16)...)
			}
			body = append(body, 0x41, 0, 0x11, 0, 0)
			if vector {
				body = append(body, 0x1a)
			}
		}
		body = append(body, 0x0b)
		functions := []byte{0}
		code := [][]byte{wasmtest.Code(leaf)}
		for range 96 {
			functions = append(functions, 1)
			code = append(code, wasmtest.Code(body))
		}
		functionSection := append(wasmtest.ULEB(uint32(len(functions))), functions...)
		source := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(values, values), wasmtest.FuncType(nil, nil))),
			wasmtest.Section(3, functionSection), wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}),
			wasmtest.Section(10, wasmtest.Vec(code...)),
		)
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		pressure := 0
		image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, MemoryPressure: func() { pressure++ }})
		if image == nil || err != nil || pressure != 1 || image.CodeImage != nil || len(image.Code) > MaxPublicCodeBytes || len(image.Entry) != len(m.Code) {
			t.Fatal("public aggregate image admission", vector, err, pressure)
		}
		t.Log("bounded public image bytes", vector, len(image.Code))
	}
}
