//go:build linux && !tinygo

package riscv64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func TestImmediateReferenceProofRejectsObjectIngress(t *testing.T) {
	makeModule := func(types, imports, globals, tables, exports []byte, body []byte) *wasm.Module {
		sections := [][]byte{wasmtest.Section(1, types)}
		if imports != nil {
			sections = append(sections, wasmtest.Section(2, imports))
		}
		sections = append(sections, wasmtest.Section(3, wasmtest.Vec([]byte{0})))
		if tables != nil {
			sections = append(sections, wasmtest.Section(4, tables))
		}
		if globals != nil {
			sections = append(sections, wasmtest.Section(6, globals))
		}
		if exports != nil {
			sections = append(sections, wasmtest.Section(7, exports))
		}
		sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
		m, err := wasm.DecodeModule(wasmtest.Module(sections...))
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	numeric := wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))
	immediate := []byte{0x41, 7, 0xfb, 0x1c, 0xfb, 0x1e, 0x0b}
	base := makeModule(numeric, nil, nil, nil, nil, immediate)
	if !admitRV64ImmediateReferenceModule(base) {
		t.Fatal("ordinary immediate source rejected")
	}
	for name, m := range map[string]*wasm.Module{
		"any-parameter":               makeModule(wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.I32})), nil, nil, nil, nil, immediate),
		"extern-parameter":            makeModule(wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.I32})), nil, nil, nil, nil, immediate),
		"imported-collector-global":   makeModule(numeric, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "object", wasm.AnyRef, false)), nil, nil, nil, immediate),
		"foreign-any-table":           makeModule(numeric, nil, nil, wasmtest.Vec([]byte{0x6e, 0, 1}), wasmtest.Vec(wasmtest.ExportEntry("table", 1, 0)), immediate),
		"foreign-any-global":          makeModule(numeric, nil, wasmtest.Vec(wasmtest.GlobalEntry(wasm.AnyRef, true, []byte{0xd0, 0x6e, 0x0b})), nil, wasmtest.Vec(wasmtest.ExportEntry("global", 3, 0)), immediate),
		"object-type":                 makeModule(wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), []byte{0x5f, 0}), nil, nil, nil, nil, immediate),
		"foreign-function-descriptor": makeModule(numeric, nil, nil, nil, nil, []byte{0xd2, 0, 0x1a, 0x41, 7, 0x0b}),
	} {
		t.Run(name, func(t *testing.T) {
			if admitRV64ImmediateReferenceModule(m) {
				t.Fatal("object-capable module entered immediate-only proof")
			}
		})
	}
	// RefNull classifier skips its heap immediate; admission must read it from
	// the original reader and must not classify ordinary function nulls as GC.
	for _, heap := range []wasm.AbsHeapType{wasm.HeapAny, wasm.HeapI31, wasm.HeapNone} {
		if !admitRV64ImmediateReferenceModule(makeModule(numeric, nil, nil, nil, nil, []byte{0xd0, byte(heap), 0x1a, 0x41, 0, 0x0b})) {
			t.Fatalf("collector null %v rejected", heap)
		}
	}
	if admitRV64ImmediateReferenceModule(makeModule(numeric, nil, nil, nil, nil, []byte{0xd0, 0x70, 0x1a, 0x41, 0, 0x0b})) {
		t.Fatal("function null selected immediate GC mode")
	}
}

func BenchmarkImmediateReferenceAdmission(b *testing.B) {
	for _, reference := range []bool{false, true} {
		b.Run(map[bool]string{false: "numeric-control", true: "immediate"}[reference], func(b *testing.B) {
			body := []byte{0x41, 7, 0x0b}
			if reference {
				body = []byte{0x41, 7, 0xfb, 0x1c, 0xfb, 0x1e, 0x0b}
			}
			source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
			m, err := wasm.DecodeModule(source)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if admitRV64ImmediateReferenceModule(m) != reference {
					b.Fatal("wrong admission result")
				}
			}
		})
	}
}
