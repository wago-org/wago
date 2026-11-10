//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestPublicLocalInitializerScope(t *testing.T) {
	for _, typ := range []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64, wasm.V128} {
		var literal []byte
		switch {
		case wasm.EqualValType(typ, wasm.I32):
			literal = []byte{0x41, 1, 0x0b}
		case wasm.EqualValType(typ, wasm.I64):
			literal = []byte{0x42, 1, 0x0b}
		case wasm.EqualValType(typ, wasm.F32):
			literal = []byte{0x43, 1, 0, 0x80, 0x7f, 0x0b}
		case wasm.EqualValType(typ, wasm.F64):
			literal = []byte{0x44, 1, 0, 0, 0, 0, 0, 0xf0, 0x7f, 0x0b}
		default:
			literal = append([]byte{0xfd, 12}, append(make([]byte, 16), 0x0b)...)
		}
		source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{typ}))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(typ, false, literal), wasmtest.GlobalEntry(typ, false, []byte{0x23, 0, 0x0b}), wasmtest.GlobalEntry(typ, true, []byte{0x23, 1, 0x0b}))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("get", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 2, 0x0b}))))
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true})
		if err != nil {
			t.Fatal(typ, err)
		}
		if image.CodeImage != nil {
			image.CodeImage.Close()
		}
		if image, err := CompileProduction(source, ProductionOptions{DeferCodeMapping: true}); image != nil || err == nil {
			if image != nil && image.CodeImage != nil {
				image.CodeImage.Close()
			}
			t.Fatal("private scope widened", typ, err)
		}
		for _, mutate := range []func(*wasm.Module){
			func(m *wasm.Module) { m.Globals[1].Init.BodyBytes = []byte{0x23, 1, 0x0b} },
			func(m *wasm.Module) { m.Globals[1].Init.BodyBytes = []byte{0x23, 2, 0x0b} },
			func(m *wasm.Module) { m.Globals[0].Type.Mutable = true },
			func(m *wasm.Module) { m.Globals[2].Init.BodyBytes = []byte{0x23, 1, 0x0b, 0x0b} },
		} {
			bad, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			mutate(bad)
			if err := AdmitPublicInitializers(bad); err == nil {
				t.Fatal("invalid scoped initializer admitted", typ)
			}
			pressure := 0
			if image, err := CompileModuleWith(bad, CompileOptions{DeferCodeMapping: true, MemoryPressure: func() { pressure++ }}); image != nil || err == nil || pressure != 0 {
				t.Fatal("invalid local initializer staged", typ, err, pressure)
			}
		}
	}
}
