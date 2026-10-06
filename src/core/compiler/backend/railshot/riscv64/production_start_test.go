//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestProductionDefinedStartAdmissionPreservesPrivateGates(t *testing.T) {
	for _, vector := range []bool{false, true} {
		result, body := wasm.I32, []byte{0x41, 7, 0x0b}
		if vector {
			result = wasm.V128
			body = append(append([]byte{0xfd, 12}, make([]byte, 16)...), 0x0b)
		}
		source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{result}))), wasmtest.Section(3, []byte{2, 0, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 1))), wasmtest.Section(8, []byte{0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code(body))))
		if err := AdmitProductionSource(source); err != nil {
			t.Fatal("public defined-start provenance", vector, err)
		}
		if image, err := CompileProduction(source, ProductionOptions{DeferCodeMapping: true, Interruptible: true}); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("byte compiler omitted start lifecycle", vector, err)
		}
		if vector {
			if image, err := compileSWARModule(source, emissionMode{production: true, interruptible: true}); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("private vector start admitted", err)
			}
		} else if image, err := Compile(source); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("private scalar start admitted", err)
		}
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
		if err != nil || image == nil || image.CodeImage != nil || len(image.Entry) != 2 {
			t.Fatal("defined start staging", vector, err)
		}
		if vector {
			if private, err := compileSWARDecodedModule(m, emissionMode{production: true, interruptible: true}, swarModulePlain); private != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("private decoded vector start admitted", err)
			}
		}
		m.Start = nil
		reference, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true})
		if err != nil || !bytes.Equal(image.Code, reference.Code) {
			t.Fatal("start changed native function lowering", vector, err)
		}
		for _, index := range []wasm.FuncIdx{1, 2, ^wasm.FuncIdx(0)} {
			m.Start = &index
			pressure := 0
			if bad, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, MemoryPressure: func() { pressure++ }}); bad != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
				t.Fatal("nonvoid/out-of-range start staged", index, err)
			}
		}
	}
}

func TestProductionDefinedStartUsesFunctionImportOrdinal(t *testing.T) {
	state := wasmtest.GlobalImportEntry("env", "v", wasm.V128, false)
	host := append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0)
	for _, entries := range [][][]byte{{state, host}, {host, state}} {
		source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(2, wasmtest.Vec(entries...)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(8, []byte{1}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))))
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, ImportBindings: hostImportBindings(1)}
		if image, err := CompileModuleWith(m, opts); image == nil || err != nil {
			t.Fatal("interleaved defined-start function ordinal", err)
		}
		*m.Start = 0
		if image, err := CompileModuleWith(m, opts); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("imported void start admitted", err)
		}
	}
}
