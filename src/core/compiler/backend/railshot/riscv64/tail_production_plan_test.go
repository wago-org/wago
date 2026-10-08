//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"errors"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestProductionTailAddressFreeFinalizedPlans(t *testing.T) {
	for _, pair := range []bool{false, true} {
		types := []wasm.ValType{wasm.I32, wasm.F32}
		if pair {
			types = []wasm.ValType{wasm.I32, wasm.V128}
		}
		source := tailImportTestSource(types, types, []byte{0x20, 0, 0x20, 1, 0x12, 0, 0x0b})
		opts := CompileOptions{NativeTailCalls: true, InvocationContext: true, Interruptible: true, DeferCodeMapping: true, ImportBindings: []ImportBinding{{Dynamic: true, ImportIndex: 0}}}
		first, err := CompileModuleWith(localTailDecode(t, source), opts)
		if err != nil {
			t.Fatal(err)
		}
		second, err := CompileModuleWith(localTailDecode(t, source), opts)
		if err != nil {
			t.Fatal(err)
		}
		if first.CodeImage != nil || first.NativeTail == nil || len(first.NativeTail.Plans) != 1 || !bytes.Equal(first.Code, second.Code) || !slices.Equal(first.NativeTail.Plans, second.NativeTail.Plans) {
			t.Fatal("symbolic deterministic production tail image")
		}
		p := first.NativeTail.Plans[0]
		stride := 8
		if pair {
			stride = 16
		}
		if p.FunctionIndex != 1 || p.ImportIndex != 0 || p.SourcePC != 5 || p.FrameBytes < 48 || p.FrameBytes&15 != 0 || p.SourceStride != stride || p.SourceBase < 48 || p.SourceBase+stride+8 > p.FrameBytes {
			t.Fatal("finalized tail source frame", p)
		}
		pressured := false
		opts.InvocationContext = false
		opts.MemoryPressure = func() { pressured = true }
		if image, err := CompileModuleWith(localTailDecode(t, source), opts); image != nil || !errors.Is(err, ErrUnsupported) || pressured {
			t.Fatal("unqualified runtime reached publication", err)
		}
	}
}

func TestProductionIndirectTailDeclaredTypeAndGates(t *testing.T) {
	for _, vector := range []bool{false, true} {
		types := []wasm.ValType{wasm.I32}
		if vector {
			types = append(types, wasm.V128)
		}
		var body []byte
		for i := range types {
			body = append(body, 0x20, byte(i))
		}
		body = append(body, 0x41, 0, 0x13, 2, 0, 0x0b)
		source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil), wasmtest.FuncType(types, types))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}), wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
		opts := CompileOptions{NativeTailCalls: true, InvocationContext: true, Interruptible: true, ForeignTableCalls: true, DeferCodeMapping: true}
		m := localTailDecode(t, source)
		a, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatal(err)
		}
		b, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatal(err)
		}
		if a.CodeImage != nil || a.NativeTail == nil || len(a.NativeTail.Plans) != 1 || !bytes.Equal(a.Code, b.Code) || !slices.Equal(a.NativeTail.Plans, b.NativeTail.Plans) {
			t.Fatal("indirect staging is not address-free and deterministic")
		}
		p := a.NativeTail.Plans[0]
		if p.Kind != 1 || p.TypeIndex != 2 || p.FunctionIndex != 0 || p.ImportIndex != 0 || p.ReferenceBase < p.SourceBase+len(types)*p.SourceStride || p.ReferenceBase > p.FrameBytes-8 {
			t.Fatal("declared type/full-width reference", p)
		}
		// Ordinary call_ref stays closed even in unreachable code, before
		// mapped-image staging in both scalar and SWAR paths.
		m.Code[0].BodyBytes = []byte{0x00, 0xd0, 0, 0x14, 0, 0x0b}
		pressured := false
		opts.MemoryPressure = func() { pressured = true }
		if image, err := CompileModuleWith(m, opts); image != nil || !errors.Is(err, ErrUnsupported) || pressured {
			t.Fatal("ordinary call_ref reached production staging", image, err)
		}
	}
}

func TestProductionReferenceTailDeclaredTypeAndGates(t *testing.T) {
	for _, vector := range []bool{false, true} {
		types := []wasm.ValType{wasm.I32, wasm.F32}
		if vector {
			types = append(types, wasm.V128)
		}
		var body []byte
		for i := range types {
			body = append(body, 0x20, byte(i))
		}
		body = append(body, 0xd2, 0, 0x15, 2, 0x0b)
		source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil), wasmtest.FuncType(types, types))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
		opts := CompileOptions{NativeTailCalls: true, InvocationContext: true, Interruptible: true, ForeignTableCalls: true, DeferCodeMapping: true}
		m := localTailDecode(t, source)
		a, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatal(err)
		}
		b, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatal(err)
		}
		if a.CodeImage != nil || a.NativeTail == nil || len(a.NativeTail.Plans) != 1 || !bytes.Equal(a.Code, b.Code) || !slices.Equal(a.NativeTail.Plans, b.NativeTail.Plans) {
			t.Fatal("reference staging is not address-free and deterministic")
		}
		p := a.NativeTail.Plans[0]
		if p.Kind != 2 || p.TypeIndex != 2 || p.FunctionIndex != 0 || p.ImportIndex != 0 || p.ReferenceBase < p.SourceBase+len(types)*p.SourceStride || p.ReferenceBase > p.FrameBytes-8 {
			t.Fatal("declared type/full-width reference", p)
		}
		opts.ForeignTableCalls = false
		if image, err := CompileModuleWith(m, opts); image != nil || !errors.Is(err, ErrUnsupported) {
			t.Fatal("reference tail without authenticated owner protocol", err)
		}
	}
}
