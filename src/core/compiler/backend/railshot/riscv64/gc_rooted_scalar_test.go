//go:build linux && !tinygo

package riscv64

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64RootedInternalReferenceCallModule(exportInternal bool) []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		{0x60, 0x01, 0x64, 0x00, 0x01, 0x64, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	callee := []byte{0x00, 0xfb, 0x01, 0x00, 0x1a, 0x20, 0x00, 0x0b}
	caller := []byte{0x01, 0x01, 0x7f,
		0x41, 0x29, 0xfb, 0x00, 0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00, 0x10, 0x00,
		0xfb, 0x02, 0x00, 0x00, 0x21, 0x00,
		0xfb, 0x02, 0x00, 0x00, 0x20, 0x00, 0x6a, 0x0b,
	}
	export := uint32(1)
	if exportInternal {
		export = 0
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), export))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(callee))), callee...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func TestRV64RootedInternalReferenceCallCompiles(t *testing.T) {
	source := rv64RootedInternalReferenceCallModule(false)
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	roots := shared.NewGCModuleFrameRootPlan(2)
	if !roots.MarkFunction(0) || !roots.MarkFunction(1) || !roots.ReserveFunctions(2) {
		t.Fatal("reserve exact root plans")
	}
	callee, ok := roots.BeginFunction(0)
	if !ok {
		t.Fatal("begin callee root plan")
	}
	*callee = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true, Locals: []shared.GCFrameLocal{{Index: 0, Offset: shared.RV64CallFrameHeaderBytes}}}
	if !callee.SetLiveMasks([]uint64{1}, 1, 0) {
		t.Fatal("install callee root masks")
	}
	caller, ok := roots.BeginFunction(1)
	if !ok {
		t.Fatal("begin caller root plan")
	}
	*caller = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true, SafepointBase: 1}
	if !caller.SetLiveMasks([]uint64{0, 0, 0}, 2, 1) {
		t.Fatal("install caller root masks")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := CompileModuleWith(m, CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCStructHelpers: true, SyncHostSlots: 4, GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Code) == 0 || callee.SafepointCount() != 1 || caller.SafepointCount() != 2 || caller.CallsiteCount() != 1 {
		t.Fatalf("code=%d callee-safepoints=%d caller-safepoints=%d callsites=%d", len(compiled.Code), callee.SafepointCount(), caller.SafepointCount(), caller.CallsiteCount())
	}
	site, ok := caller.Callsite(0)
	if !ok || site.ReturnOffset() == 0 || site.StackAdjust() != 0 || len(site.Offsets()) != 1 {
		t.Fatalf("caller callsite = %+v ok=%t", site, ok)
	}
}

func TestRV64RootedReferenceExportRejected(t *testing.T) {
	if err := AdmitProductionSource(rv64RootedInternalReferenceCallModule(true)); err == nil {
		t.Fatal("collector reference export unexpectedly admitted")
	}
}

func TestRV64RootedAggregateTableBulkCompilesToHelpers(t *testing.T) {
	body := []byte{
		0x00,
		0x41, 0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0x26, 0x00,
		0x41, 0x00, 0x41, 0x07, 0xfb, 0x00, 0x00, 0x41, 0x01, 0xfc, 0x11, 0x00,
		0x41, 0x01, 0x41, 0x00, 0x41, 0x01, 0xfc, 0x0e, 0x00, 0x00,
		0xd0, 0x00, 0x41, 0x01, 0xfc, 0x0f, 0x00, 0x1a,
		0x41, 0x00, 0x41, 0x00, 0x41, 0x01, 0xfc, 0x0c, 0x00, 0x00,
		0xfc, 0x0d, 0x00,
		0x0b,
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, []byte{0x01, 0x63, 0x00, 0x01, 0x02, 0x04}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(9, []byte{0x01, 0x05, 0x63, 0x00, 0x01, 0x41, 0x37, 0xfb, 0x00, 0x00, 0x0b}),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	active, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	active.Elements[0].Mode.Kind = wasm.ElemActive
	active.Elements[0].Mode.Offset = wasm.Expr{BodyBytes: []byte{0x41, 0x00, 0x0b}}
	if !admitRV64GCRootedScalarModule(active) {
		t.Fatal("active aggregate element shape was not admitted")
	}
	defaulted, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	defaulted.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0xfb, 0x01, 0x00, 0x0b}
	if !admitRV64GCRootedScalarModule(defaulted) {
		t.Fatal("struct.new_default aggregate element was not admitted")
	}
	declarative, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	declarative.Elements[0].Mode.Kind = wasm.ElemDeclarative
	if !admitRV64GCRootedScalarModule(declarative) {
		t.Fatal("declarative aggregate element shape was not admitted")
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"wrong-null-heap": func(candidate *wasm.Module) {
			candidate.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0xd0, 0x01, 0x0b}
		},
		"wrong-constructor-type": func(candidate *wasm.Module) {
			candidate.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0x41, 0x37, 0xfb, 0x00, 0x01, 0x0b}
		},
		"wrong-field-literal": func(candidate *wasm.Module) {
			candidate.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0x42, 0x37, 0xfb, 0x00, 0x00, 0x0b}
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			mutate(candidate)
			if admitRV64GCRootedScalarModule(candidate) {
				t.Fatal("unsupported aggregate element shape was admitted")
			}
		})
	}
	if got := rootedScalarHelperSlots(m); got != 5 {
		t.Fatalf("aggregate table helper slots = %d, want 5", got)
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	roots := shared.NewGCModuleFrameRootPlan(1)
	if !roots.MarkFunction(0) || !roots.ReserveFunctions(1) {
		t.Fatal("reserve exact root plan")
	}
	plan, ok := roots.BeginFunction(0)
	if !ok {
		t.Fatal("begin exact root plan")
	}
	*plan = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true}
	if !plan.SetLiveMasks([]uint64{0, 0}, 2, 0) {
		t.Fatal("install exact root liveness")
	}
	compiled, err := compileDecodedModule(m, scalarRegisters[:], CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCStructHelpers: true, SyncHostSlots: 64, GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen:    codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
		production: true, publicStarts: true, gcRootedScalar: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.code) == 0 || len(compiled.functions) != 1 || compiled.functions[0].DirectCalls != 8 || plan.SafepointCount() != 2 {
		t.Fatalf("aggregate bulk code/functions/calls/safepoints = %d/%d/%d/%d", len(compiled.code), len(compiled.functions), compiled.functions[0].DirectCalls, plan.SafepointCount())
	}
}

func TestRV64RootedAggregateArrayActiveElementCompiles(t *testing.T) {
	body := []byte{
		0x00,
		0x41, 0x01, 0x41, 0x02, 0xfb, 0x08, 0x00, 0x02, 0x1a,
		0x0b,
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5e, 0x7f, 0x01},
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, []byte{0x01, 0x63, 0x00, 0x01, 0x01, 0x04}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(9, []byte{0x01, 0x06, 0x00, 0x41, 0x00, 0x0b, 0x63, 0x00, 0x01,
			0x41, 0x0b, 0x41, 0x16, 0xfb, 0x08, 0x00, 0x02, 0x0b}),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	empty, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	empty.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0xfb, 0x08, 0x00, 0x00, 0x0b}
	if !admitRV64GCRootedScalarModule(empty) {
		t.Fatal("empty array.new_fixed element was not admitted")
	}
	for name, body := range map[string][]byte{
		"new":         {0x41, 0x07, 0x41, 0x03, 0xfb, 0x06, 0x00, 0x0b},
		"default":     {0x41, 0x02, 0xfb, 0x07, 0x00, 0x0b},
		"default-max": {0x41, 0x80, 0x02, 0xfb, 0x07, 0x00, 0x0b},
	} {
		t.Run(name, func(t *testing.T) {
			candidate, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			candidate.Elements[0].Kind.Exprs[0].BodyBytes = body
			if !admitRV64GCRootedScalarModule(candidate) {
				t.Fatalf("array.%s element was not admitted", name)
			}
		})
	}
	for name, replace := range map[string][]byte{
		"wrong-count":         {0x41, 0x0b, 0x41, 0x16, 0xfb, 0x08, 0x00, 0x01, 0x0b},
		"wrong-op":            {0x41, 0x0b, 0x41, 0x16, 0xfb, 0x06, 0x00, 0x02, 0x0b},
		"wrong-type":          {0x41, 0x0b, 0x41, 0x16, 0xfb, 0x08, 0x01, 0x02, 0x0b},
		"wrong-value":         {0x42, 0x0b, 0x41, 0x16, 0xfb, 0x08, 0x00, 0x02, 0x0b},
		"new-negative-length": {0x41, 0x07, 0x41, 0x7f, 0xfb, 0x06, 0x00, 0x0b},
		"new-oversize-length": {0x41, 0x07, 0x41, 0x81, 0x02, 0xfb, 0x06, 0x00, 0x0b},
		"default-negative":    {0x41, 0x7f, 0xfb, 0x07, 0x00, 0x0b},
		"default-oversize":    {0x41, 0x81, 0x02, 0xfb, 0x07, 0x00, 0x0b},
	} {
		t.Run(name, func(t *testing.T) {
			candidate, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			candidate.Elements[0].Kind.Exprs[0].BodyBytes = replace
			if admitRV64GCRootedScalarModule(candidate) {
				t.Fatal("malformed array constructor element was admitted")
			}
		})
	}
}

func TestRV64RootedAggregateNullableReferenceElements(t *testing.T) {
	tests := map[string]struct {
		container []byte
		entries   []byte
		wrong     []byte
		count     byte
	}{
		"struct": {
			container: []byte{0x5f, 0x01, 0x63, 0x00, 0x01}, // mutable (ref null 0) field
			entries: []byte{
				0xd0, 0x00, 0xfb, 0x00, 0x01, 0x0b, // ref.null 0; struct.new 1
				0xfb, 0x01, 0x01, 0x0b, // struct.new_default 1
			},
			wrong: []byte{0xd0, 0x70, 0xfb, 0x00, 0x01, 0x0b}, // funcref null is not a struct ref
			count: 2,
		},
		"array": {
			container: []byte{0x5e, 0x63, 0x00, 0x01}, // mutable (ref null 0) elements
			entries: []byte{
				0xd0, 0x00, 0xd0, 0x00, 0xfb, 0x08, 0x01, 0x02, 0x0b, // array.new_fixed 1
				0xd0, 0x00, 0x41, 0x02, 0xfb, 0x06, 0x01, 0x0b, // array.new 1
				0x41, 0x02, 0xfb, 0x07, 0x01, 0x0b, // array.new_default 1
			},
			wrong: []byte{0xd0, 0x70, 0xd0, 0x00, 0xfb, 0x08, 0x01, 0x02, 0x0b},
			count: 3,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			elements := []byte{0x01, 0x06, 0x00, 0x41, 0x00, 0x0b, 0x63, 0x01, test.count}
			elements = append(elements, test.entries...)
			body := []byte{0x00, 0x0b}
			source := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(
					[]byte{0x5f, 0x01, 0x7f, 0x01}, // mutable i32 leaf
					test.container,
					wasmtest.FuncType(nil, nil),
				)),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
				wasmtest.Section(4, []byte{0x01, 0x63, 0x01, 0x01, test.count, 0x04}),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
				wasmtest.Section(9, elements),
				wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
			)
			if err := AdmitProductionSource(source); err != nil {
				t.Fatal(err)
			}
			candidate, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			candidate.Elements[0].Kind.Exprs[0].BodyBytes = test.wrong
			if admitRV64GCRootedScalarModule(candidate) {
				t.Fatal("aggregate constructor admitted an incompatible funcref null")
			}
		})
	}
}

func TestRV64RootedAggregateLocalFunctionReferenceElement(t *testing.T) {
	body := []byte{0x00, 0x41, 0x01, 0xfb, 0x00, 0x01, 0x1a, 0x0b}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x64, 0x70, 0x01}, // mutable (ref func) field
			[]byte{0x5f, 0x01, 0x7f, 0x01},       // allocation/safepoint witness
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(4, []byte{0x01, 0x63, 0x00, 0x01, 0x01, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(9, []byte{0x01, 0x06, 0x00, 0x41, 0x00, 0x0b, 0x63, 0x00, 0x01,
			0xd2, 0x00, 0xfb, 0x00, 0x00, 0x0b}),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	candidate, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := frontend.BuildGCTypeMetadata(candidate)
	if err != nil {
		t.Fatal(err)
	}
	roots := shared.NewGCModuleFrameRootPlan(1)
	if !roots.MarkFunction(0) || !roots.ReserveFunctions(1) {
		t.Fatal("reserve exact root plan")
	}
	plan, ok := roots.BeginFunction(0)
	if !ok {
		t.Fatal("begin exact root plan")
	}
	*plan = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true}
	if !plan.SetLiveMasks([]uint64{0}, 1, 0) {
		t.Fatal("install exact root liveness")
	}
	compiled, err := CompileModuleWith(candidate, CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCStructHelpers: true, SyncHostSlots: 4, GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Code) == 0 || plan.SafepointCount() != 1 {
		t.Fatalf("code bytes=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
	}
	candidate.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0xd2, 0x01, 0xfb, 0x00, 0x00, 0x0b}
	if admitRV64GCRootedScalarModule(candidate) {
		t.Fatal("out-of-range aggregate ref.func operand was admitted")
	}
}

func TestRV64RootedAggregateRejectsImportedFunctionReferenceElement(t *testing.T) {
	functionImport := append(wasmtest.Name("env"), wasmtest.Name("f")...)
	functionImport = append(functionImport, byte(wasm.ExternFunc))
	functionImport = append(functionImport, wasmtest.ULEB(2)...)
	body := []byte{0x00, 0x41, 0x01, 0xfb, 0x00, 0x01, 0x1a, 0x0b}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x64, 0x70, 0x01}, // mutable (ref func) field
			[]byte{0x5f, 0x01, 0x7f, 0x01},       // allocation/safepoint witness
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(functionImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(4, []byte{0x01, 0x63, 0x00, 0x01, 0x01, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(9, []byte{0x01, 0x06, 0x00, 0x41, 0x00, 0x0b, 0x63, 0x00, 0x01,
			0xd2, 0x01, 0xfb, 0x00, 0x00, 0x0b}),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	candidate, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	candidate.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0xd2, 0x00, 0xfb, 0x00, 0x00, 0x0b}
	if admitRV64GCRootedScalarModule(candidate) {
		t.Fatal("imported aggregate ref.func operand escaped private-table isolation")
	}
}

func TestRV64RootedAggregateNestedDefaultReferenceElements(t *testing.T) {
	tests := map[string]struct {
		types   [][]byte
		entries []byte
		count   byte
	}{
		"struct": {
			types: [][]byte{
				{0x5f, 0x01, 0x7f, 0x01},       // mutable i32 leaf
				{0x5f, 0x01, 0x64, 0x00, 0x01}, // mutable non-null (ref 0) holder
			},
			entries: []byte{
				0xfb, 0x01, 0x00, 0xfb, 0x00, 0x01, 0x0b,
				0x41, 0x2a, 0xfb, 0x00, 0x00, 0xfb, 0x00, 0x01, 0x0b,
			},
			count: 2,
		},
		"array": {
			types: [][]byte{
				{0x5e, 0x7f, 0x01},       // mutable i32 inner array
				{0x5e, 0x64, 0x00, 0x01}, // mutable non-null (ref 0) outer array
			},
			entries: []byte{
				0x41, 0x02, 0xfb, 0x07, 0x00, 0x41, 0x03, 0xfb, 0x07, 0x00, 0xfb, 0x08, 0x01, 0x02, 0x0b,
				0x41, 0x01, 0xfb, 0x07, 0x00, 0x41, 0x02, 0xfb, 0x06, 0x01, 0x0b,
				0x41, 0x05, 0x41, 0x06, 0xfb, 0x08, 0x00, 0x02,
				0x41, 0x07, 0x41, 0x02, 0xfb, 0x06, 0x00,
				0xfb, 0x08, 0x01, 0x02, 0x0b,
			},
			count: 3,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			types := append([][]byte(nil), test.types...)
			types = append(types, wasmtest.FuncType(nil, nil))
			elements := []byte{0x01, 0x06, 0x00, 0x41, 0x00, 0x0b, 0x63, 0x01, test.count}
			elements = append(elements, test.entries...)
			body := []byte{0x00, 0x0b}
			source := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(types...)),
				wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
				wasmtest.Section(4, []byte{0x01, 0x63, 0x01, 0x01, test.count, 0x04}),
				wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
				wasmtest.Section(9, elements),
				wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
			)
			if err := AdmitProductionSource(source); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRV64RootedAggregateNestedAllocationBound(t *testing.T) {
	body := []byte{0x00, 0x0b}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01}, // mutable i32 leaf struct
			[]byte{0x5e, 0x64, 0x00, 0x01}, // mutable non-null (ref 0) outer array
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(4, []byte{0x01, 0x63, 0x01, 0x01, 0x01, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(9, []byte{0x01, 0x06, 0x00, 0x41, 0x00, 0x0b, 0x63, 0x01, 0x01,
			0xfb, 0x01, 0x00, 0xfb, 0x08, 0x01, 0x01, 0x0b}),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	for _, count := range []uint32{255, 256} {
		candidate, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		expr := make([]byte, 0, int(count)*3+8)
		for range count {
			expr = append(expr, 0xfb, 0x01, 0x00)
		}
		expr = append(expr, 0xfb, 0x08, 0x01)
		expr = append(expr, wasmtest.ULEB(count)...)
		expr = append(expr, 0x0b)
		candidate.Elements[0].Kind.Exprs[0].BodyBytes = expr
		if got, want := admitRV64GCRootedScalarModule(candidate), count == 255; got != want {
			t.Fatalf("nested allocation count %d admitted = %v, want %v", count, got, want)
		}
	}
}

func TestRV64RootedAggregateNestedDepthBound(t *testing.T) {
	for _, total := range []uint32{16, 17} {
		types := make([][]byte, 0, total+1)
		types = append(types, []byte{0x5f, 0x01, 0x7f, 0x01})
		for index := uint32(1); index < total; index++ {
			typ := []byte{0x5f, 0x01, 0x64}
			typ = append(typ, wasmtest.SLEB32(int32(index-1))...)
			typ = append(typ, 0x01)
			types = append(types, typ)
		}
		types = append(types, wasmtest.FuncType(nil, nil))
		expr := []byte{0x41, 0x00, 0xfb, 0x00, 0x00}
		for index := uint32(1); index < total; index++ {
			expr = append(expr, 0xfb, 0x00)
			expr = append(expr, wasmtest.ULEB(index)...)
		}
		expr = append(expr, 0x0b)
		elements := []byte{0x01, 0x06, 0x00, 0x41, 0x00, 0x0b, 0x63}
		elements = append(elements, wasmtest.SLEB32(int32(total-1))...)
		elements = append(elements, 0x01)
		elements = append(elements, expr...)
		body := []byte{0x00, 0x0b}
		table := []byte{0x01, 0x63}
		table = append(table, wasmtest.SLEB32(int32(total-1))...)
		table = append(table, 0x01, 0x01, 0x01)
		source := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(types...)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(total))),
			wasmtest.Section(4, table),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
			wasmtest.Section(9, elements),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
		)
		candidate, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := admitRV64GCRootedScalarModule(candidate), total == 16; got != want {
			t.Fatalf("aggregate nesting depth %d admitted = %v, want %v", total, got, want)
		}
	}
}

func rv64RootedTypedSelectModule(typeIndex uint32, nullable, same bool) []byte {
	types := make([][]byte, typeIndex+1)
	for i := range types {
		types[i] = []byte{0x5f, 0x01, 0x7f, 0x00} // immutable struct (field i32)
	}
	types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))
	ref := append([]byte{0x64}, wasmtest.SLEB32(int32(typeIndex))...)
	body := append([]byte{0x01, 0x02}, ref...)
	body = append(body, 0x41, 0x0b, 0xfb, 0x00)
	body = append(body, wasmtest.ULEB(typeIndex)...)
	body = append(body, 0x21, 0x01, 0x41, 0x16, 0xfb, 0x00)
	body = append(body, wasmtest.ULEB(typeIndex)...)
	body = append(body, 0x21, 0x02, 0x20, 0x01, 0x20)
	if same {
		body = append(body, 0x01)
	} else {
		body = append(body, 0x02)
	}
	body = append(body, 0x20, 0x00, 0x1c, 0x01)
	if nullable {
		ref[0] = 0x63
	}
	body = append(body, ref...)
	if same {
		body = append(body, 0x20, 0x01, 0xd3)
	} else {
		body = append(body, 0xfb, 0x02)
		body = append(body, wasmtest.ULEB(typeIndex)...)
		body = append(body, 0x00)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(typeIndex+1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rv64RootedTypedSelectPlan(t *testing.T, m *wasm.Module) *shared.GCModuleFrameRootPlan {
	t.Helper()
	roots := shared.NewGCModuleFrameRootPlan(len(m.Code))
	if !roots.MarkFunction(0) || !roots.ReserveFunctions(1) {
		t.Fatal("reserve exact root plan")
	}
	plan, ok := roots.BeginFunction(0)
	if !ok {
		t.Fatal("begin exact root plan")
	}
	*plan = shared.GCFrameRootPlan{
		Candidate: true, Exact: true, Conservative: true,
		Locals: []shared.GCFrameLocal{{Index: 1, Offset: 56}, {Index: 2, Offset: 64}},
	}
	if !plan.SetLiveMasks([]uint64{3, 3}, 2, 0) {
		t.Fatal("install exact root liveness")
	}
	return roots
}

func TestRV64RootedScalarTypedSelectCompiles(t *testing.T) {
	for _, index := range []uint32{0, 130} {
		for _, nullable := range []bool{false, true} {
			for _, same := range []bool{false, true} {
				t.Run(fmt.Sprintf("index=%d/nullable=%t/same=%t", index, nullable, same), func(t *testing.T) {
					source := rv64RootedTypedSelectModule(index, nullable, same)
					if err := AdmitProductionSource(source); err != nil {
						t.Fatal(err)
					}
					m, err := wasm.DecodeModule(source)
					if err != nil {
						t.Fatal(err)
					}
					metadata, err := frontend.BuildGCTypeMetadata(m)
					if err != nil {
						t.Fatal(err)
					}
					roots := rv64RootedTypedSelectPlan(t, m)
					compiled, err := CompileModuleWith(m, CompileOptions{
						DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
						GCStructHelpers: true, SyncHostSlots: 4, GCFrameRoots: roots, GCFrameRootsSource: true,
						Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
					})
					if err != nil {
						t.Fatal(err)
					}
					if len(compiled.Code) == 0 || roots.Function(0).SafepointCount() != 2 {
						t.Fatalf("code bytes=%d safepoints=%d", len(compiled.Code), roots.Function(0).SafepointCount())
					}
				})
			}
		}
	}
}

func TestRV64RootedScalarMutableStructCompiles(t *testing.T) {
	types := [][]byte{
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		{0x5f, 0x01, 0x7f, 0x01},
	}
	body := []byte{0x01, 0x01, 0x64, 0x01, 0x41, 0x0b, 0xfb, 0x00, 0x01, 0x21, 0x00, 0x20, 0x00, 0x41, 0x2a, 0xfb, 0x05, 0x01, 0x00, 0x20, 0x00, 0xfb, 0x02, 0x01, 0x00, 0x0b}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	roots := shared.NewGCModuleFrameRootPlan(1)
	if !roots.MarkFunction(0) || !roots.ReserveFunctions(1) {
		t.Fatal("reserve exact root plan")
	}
	plan, ok := roots.BeginFunction(0)
	if !ok {
		t.Fatal("begin exact root plan")
	}
	*plan = shared.GCFrameRootPlan{
		Candidate: true, Exact: true, Conservative: true,
		Locals: []shared.GCFrameLocal{{Index: 0, Offset: shared.RV64CallFrameHeaderBytes}},
	}
	if !plan.SetLiveMasks([]uint64{1}, 1, 0) {
		t.Fatal("install exact root liveness")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := CompileModuleWith(m, CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCStructHelpers: true, SyncHostSlots: 4, GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Code) == 0 || plan.SafepointCount() != 1 {
		t.Fatalf("code bytes=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
	}
}

func TestRV64RootedScalarMultiFieldStructCompiles(t *testing.T) {
	types := [][]byte{
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		{0x5f, 0x05, 0x7f, 0x00, 0x7f, 0x00, 0x7f, 0x00, 0x7f, 0x01, 0x7f, 0x00},
	}
	body := []byte{
		0x01, 0x01, 0x64, 0x01,
		0x41, 0x01, 0x41, 0x02, 0x41, 0x03, 0x41, 0x04, 0x41, 0x05, 0xfb, 0x00, 0x01, 0x21, 0x00,
		0x20, 0x00, 0x41, 0x28, 0xfb, 0x05, 0x01, 0x03,
		0x20, 0x00, 0xfb, 0x02, 0x01, 0x00,
		0x20, 0x00, 0xfb, 0x02, 0x01, 0x03, 0x6a,
		0x20, 0x00, 0xfb, 0x02, 0x01, 0x04, 0x6a, 0x0b,
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	roots := shared.NewGCModuleFrameRootPlan(1)
	if !roots.MarkFunction(0) || !roots.ReserveFunctions(1) {
		t.Fatal("reserve exact root plan")
	}
	plan, ok := roots.BeginFunction(0)
	if !ok {
		t.Fatal("begin exact root plan")
	}
	*plan = shared.GCFrameRootPlan{
		Candidate: true, Exact: true, Conservative: true,
		Locals: []shared.GCFrameLocal{{Index: 0, Offset: shared.RV64CallFrameHeaderBytes}},
	}
	if !plan.SetLiveMasks([]uint64{1}, 1, 0) {
		t.Fatal("install exact root liveness")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := CompileModuleWith(m, CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCStructHelpers: true, SyncHostSlots: 6, GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Code) == 0 || plan.SafepointCount() != 1 {
		t.Fatalf("code bytes=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
	}
}

func rootedMixedScalarStructModule() []byte {
	types := [][]byte{
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}),
		{0x5f, 0x04, 0x7f, 0x00, 0x7e, 0x01, 0x7d, 0x00, 0x7c, 0x00},
	}
	body := []byte{
		0x01, 0x01, 0x64, 0x01,
		0x41, 0x01,
		0x42, 0x02,
		0x43, 0x00, 0x00, 0x50, 0x40,
		0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x12, 0x40,
		0xfb, 0x00, 0x01, 0x21, 0x00,
		0x20, 0x00, 0x42, 0x2a, 0xfb, 0x05, 0x01, 0x01,
		0x20, 0x00, 0xfb, 0x02, 0x01, 0x00,
		0x20, 0x00, 0xfb, 0x02, 0x01, 0x01,
		0x20, 0x00, 0xfb, 0x02, 0x01, 0x02,
		0x20, 0x00, 0xfb, 0x02, 0x01, 0x03,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedMixedScalarStructCompiles(t *testing.T) {
	source := rootedMixedScalarStructModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	roots := shared.NewGCModuleFrameRootPlan(1)
	if !roots.MarkFunction(0) || !roots.ReserveFunctions(1) {
		t.Fatal("reserve exact root plan")
	}
	plan, ok := roots.BeginFunction(0)
	if !ok {
		t.Fatal("begin exact root plan")
	}
	*plan = shared.GCFrameRootPlan{
		Candidate: true, Exact: true, Conservative: true,
		Locals: []shared.GCFrameLocal{{Index: 0, Offset: shared.RV64CallFrameHeaderBytes}},
	}
	if !plan.SetLiveMasks([]uint64{1}, 1, 0) {
		t.Fatal("install exact root liveness")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := CompileModuleWith(m, CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCStructHelpers: true, SyncHostSlots: 5, GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Code) == 0 || plan.SafepointCount() != 1 {
		t.Fatalf("code bytes=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
	}
}

func rootedPackedScalarStructModule() []byte {
	types := [][]byte{
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}),
		{0x5f, 0x02, byte(wasm.PackI8), 0x01, byte(wasm.PackI16), 0x01},
	}
	body := []byte{
		0x01, 0x01, 0x64, 0x01,
		0x41, 0x01, 0x41, 0x02, 0xfb, 0x00, 0x01, 0x21, 0x00,
		0x20, 0x00, 0x41, 0xff, 0x01, 0xfb, 0x05, 0x01, 0x00,
		0x20, 0x00, 0x41, 0xff, 0xff, 0x03, 0xfb, 0x05, 0x01, 0x01,
		0x20, 0x00, 0xfb, 0x03, 0x01, 0x00,
		0x20, 0x00, 0xfb, 0x04, 0x01, 0x00,
		0x20, 0x00, 0xfb, 0x03, 0x01, 0x01,
		0x20, 0x00, 0xfb, 0x04, 0x01, 0x01,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedPackedScalarStructCompiles(t *testing.T) {
	source := rootedPackedScalarStructModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	roots := shared.NewGCModuleFrameRootPlan(1)
	if !roots.MarkFunction(0) || !roots.ReserveFunctions(1) {
		t.Fatal("reserve exact root plan")
	}
	plan, ok := roots.BeginFunction(0)
	if !ok {
		t.Fatal("begin exact root plan")
	}
	*plan = shared.GCFrameRootPlan{
		Candidate: true, Exact: true, Conservative: true,
		Locals: []shared.GCFrameLocal{{Index: 0, Offset: shared.RV64CallFrameHeaderBytes}},
	}
	if !plan.SetLiveMasks([]uint64{1}, 1, 0) {
		t.Fatal("install exact root liveness")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := CompileModuleWith(m, CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCStructHelpers: true, SyncHostSlots: 4, GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Code) == 0 || plan.SafepointCount() != 1 {
		t.Fatalf("code bytes=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
	}
}

func rootedWideScalarArrayModule() []byte {
	types := [][]byte{
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I64, wasm.I32}),
		{0x5e, 0x7e, 0x01},
	}
	body := []byte{
		0x01, 0x01, 0x64, 0x01,
		0x42, 0x07, 0x41, 0x02, 0xfb, 0x06, 0x01, 0x21, 0x00,
		0x20, 0x00, 0x41, 0x01, 0x42, 0x2a, 0xfb, 0x0e, 0x01,
		0x20, 0x00, 0x41, 0x01, 0xfb, 0x0b, 0x01,
		0x20, 0x00, 0xfb, 0x0f,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedWideScalarArrayCompiles(t *testing.T) {
	source := rootedWideScalarArrayModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	roots := shared.NewGCModuleFrameRootPlan(1)
	if !roots.MarkFunction(0) || !roots.ReserveFunctions(1) {
		t.Fatal("reserve exact root plan")
	}
	plan, ok := roots.BeginFunction(0)
	if !ok {
		t.Fatal("begin exact root plan")
	}
	*plan = shared.GCFrameRootPlan{
		Candidate: true, Exact: true, Conservative: true,
		Locals: []shared.GCFrameLocal{{Index: 0, Offset: shared.RV64CallFrameHeaderBytes}},
	}
	if !plan.SetLiveMasks([]uint64{1}, 1, 0) {
		t.Fatal("install exact root liveness")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := CompileModuleWith(m, CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCArrayHelpers: true, SyncHostSlots: 4, GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Code) == 0 || plan.SafepointCount() != 1 {
		t.Fatalf("code bytes=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
	}
}

func TestRV64RootedScalarArrayCopyRejectsMismatchedStorage(t *testing.T) {
	types := [][]byte{
		wasmtest.FuncType(nil, nil),
		{0x5e, 0x7e, 0x01}, // mutable i64 array
		{0x5e, 0x7f, 0x01}, // mutable i32 array
	}
	body := []byte{
		0x01, 0x02, 0x64, 0x01,
		0x41, 0x00, 0xfb, 0x07, 0x01, 0x21, 0x00,
		0x41, 0x00, 0xfb, 0x07, 0x02, 0x21, 0x01,
		0x20, 0x00, 0x41, 0x00,
		0x20, 0x01, 0x41, 0x00,
		0x41, 0x00, 0xfb, 0x11, 0x01, 0x02,
		0x0b,
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCArrayHelpers: true, SyncHostSlots: 7,
		Codegen:    codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
		production: true, publicStarts: true, gcRootedScalar: true,
	}
	_, err = compileDecodedModule(m, scalarRegisters[:], opts)
	if err == nil || !strings.Contains(err.Error(), "array.copy outside exact packed copy profile") {
		t.Fatalf("mismatched array.copy error = %v", err)
	}
}

func TestRV64RootedReferenceFieldRejectsNonDefaultableStructDefault(t *testing.T) {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x01},       // mutable i32 leaf
		{0x5f, 0x01, 0x64, 0x00, 0x01}, // mutable non-null (ref 0) holder
		wasmtest.FuncType(nil, nil),
	}
	body := []byte{0x00, 0xfb, 0x01, 0x01, 0x1a, 0x0b}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCStructHelpers: true, SyncHostSlots: 64,
		Codegen:    codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
		production: true, publicStarts: true, gcRootedScalar: true,
	}
	_, err = compileDecodedModule(m, scalarRegisters[:], opts)
	if err == nil || !strings.Contains(err.Error(), "struct.new_default outside exact GC profile") {
		t.Fatalf("non-defaultable struct.new_default error = %v", err)
	}
}

func TestRV64RootedReferenceArrayRejectsNonDefaultableArrayDefault(t *testing.T) {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x01}, // mutable i32 leaf
		{0x5e, 0x64, 0x00, 0x01}, // mutable array of non-null (ref 0)
		wasmtest.FuncType(nil, nil),
	}
	body := []byte{0x00, 0x41, 0x00, 0xfb, 0x07, 0x01, 0x1a, 0x0b}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCArrayHelpers: true, SyncHostSlots: 64,
		Codegen:    codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
		production: true, publicStarts: true, gcRootedScalar: true,
	}
	_, err = compileDecodedModule(m, scalarRegisters[:], opts)
	if err == nil || !strings.Contains(err.Error(), "array.new_default outside exact GC profile") {
		t.Fatalf("non-defaultable array.new_default error = %v", err)
	}
}

func TestRV64RootedScalarTypeWireDistinguishesFieldCountFromRecGroup(t *testing.T) {
	fields := []byte{0x5f}
	fields = append(fields, wasmtest.ULEB(78)...)
	for range 78 {
		fields = append(fields, 0x7f, 0x00)
	}
	function := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	body := []byte{0x00, 0xfb, 0x01, 0x00, 0xfb, 0x02, 0x00, 0x4d, 0x0b}
	module := func(types ...[]byte) []byte {
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(types...)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
		)
	}
	if err := AdmitProductionSource(module(fields, function)); err != nil {
		t.Fatalf("78-field struct source rejected: %v", err)
	}
	explicitRec := append([]byte{0x4e, 0x01}, fields...)
	if err := AdmitProductionSource(module(explicitRec, function)); err == nil {
		t.Fatal("explicit rec-group wrapper escaped rooted scalar wire admission")
	}
}

func TestRV64RootedScalarWireAcceptsZeroDataCount(t *testing.T) {
	body := []byte{0x00, 0x41, 0x00, 0xfb, 0x00, 0x00, 0x1a, 0x0b}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(12, wasmtest.ULEB(0)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	if err := AdmitProductionSource(source); err != nil {
		t.Fatalf("zero data-count source rejected: %v", err)
	}
}

func TestRV64RootedScalarArbitraryStructDefault(t *testing.T) {
	const typeIndex = uint32(130)
	types := make([][]byte, typeIndex+1)
	for i := range types {
		types[i] = []byte{0x5f, 0x01, 0x7f, 0x00}
	}
	types = append(types, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))
	body := []byte{0x00, 0xfb, 0x01}
	body = append(body, wasmtest.ULEB(typeIndex)...)
	body = append(body, 0xfb, 0x02)
	body = append(body, wasmtest.ULEB(typeIndex)...)
	body = append(body, 0x00, 0x0b)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(typeIndex+1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	roots := shared.NewGCModuleFrameRootPlan(1)
	if !roots.MarkFunction(0) || !roots.ReserveFunctions(1) {
		t.Fatal("reserve exact root plan")
	}
	plan, ok := roots.BeginFunction(0)
	if !ok {
		t.Fatal("begin exact root plan")
	}
	*plan = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true}
	if !plan.SetLiveMasks([]uint64{0}, 1, 0) {
		t.Fatal("install exact root liveness")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := CompileModuleWith(m, CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCStructHelpers: true, SyncHostSlots: 4, GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Code) == 0 || plan.SafepointCount() != 1 {
		t.Fatalf("code bytes=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
	}
}

func TestRV64RootedScalarRejectsI31Instructions(t *testing.T) {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	body := []byte{0x00, 0xfb, 0x01, 0x00, 0x1a, 0x41, 0x01, 0xfb, 0x1c, 0xfb, 0x1e, 0x0b}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	roots := shared.NewGCModuleFrameRootPlan(1)
	if !roots.MarkFunction(0) || !roots.ReserveFunctions(1) {
		t.Fatal("reserve exact root plan")
	}
	plan, ok := roots.BeginFunction(0)
	if !ok {
		t.Fatal("begin exact root plan")
	}
	*plan = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true}
	if !plan.SetLiveMasks([]uint64{0}, 1, 0) {
		t.Fatal("install exact root liveness")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CompileModuleWith(m, CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCStructHelpers: true, SyncHostSlots: 4, GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err == nil || !strings.Contains(err.Error(), "ref.i31 outside exact i31 profile") {
		t.Fatalf("generic rooted i31 admission = %v", err)
	}
}
