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

func TestRV64RootedReferenceExportAdmitted(t *testing.T) {
	if err := AdmitProductionSource(rv64RootedInternalReferenceCallModule(true)); err != nil {
		t.Fatalf("collector reference export was not admitted: %v", err)
	}
}

func TestRV64RootedAbstractReferenceExportRejected(t *testing.T) {
	body := []byte{0x00, 0x20, 0x00, 0x0b}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.AnyRef}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("echo", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	if err := AdmitProductionSource(source); err == nil {
		t.Fatal("mixed-representation abstract reference export unexpectedly admitted")
	}
}

func rv64RootedI31GlobalModule() []byte {
	body := []byte{
		0x00,
		0x41, 0x7e, 0xfb, 0x1c, 0x24, 0x00,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		0x23, 0x00, 0xfb, 0x1d,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I31Ref, true, []byte{0x41, 0x7f, 0xfb, 0x1c, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedI31GlobalCompiles(t *testing.T) {
	source := rv64RootedI31GlobalModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("rooted i31 global was not admitted")
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
	if !plan.SetLiveMasks([]uint64{0}, 1, 0) {
		t.Fatal("install exact root liveness")
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
		t.Fatalf("code=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
	}
	m.Globals[0].Type.Type = wasm.AnyRef
	if admitRV64GCRootedScalarModule(m) {
		t.Fatal("abstract anyref global escaped rooted i31 admission")
	}
}

func rv64RootedI31TableModule(exportTable bool) []byte {
	body := []byte{
		0x00,
		0x41, 0x00, 0x41, 0x7e, 0xfb, 0x1c, 0x26, 0x00,
		0x41, 0x01, 0x41, 0x7d, 0xfb, 0x1c, 0x41, 0x01, 0xfc, 0x11, 0x00,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		0x41, 0x00, 0x25, 0x00, 0xfb, 0x1d,
		0x41, 0x01, 0x25, 0x00, 0xfb, 0x1d,
		0x41, 0x7c, 0xfb, 0x1c, 0x41, 0x01, 0xfc, 0x0f, 0x00,
		0x41, 0x02, 0x25, 0x00, 0xfb, 0x1d,
		0x41, 0x00, 0x41, 0x02, 0x41, 0x01, 0xfc, 0x0e, 0x00, 0x00,
		0x41, 0x00, 0x25, 0x00, 0xfb, 0x1d,
		0x0b,
	}
	exports := [][]byte{wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0)}
	if exportTable {
		exports = append(exports, wasmtest.ExportEntry("values", byte(wasm.ExternTable), 0))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, []byte{0x01, byte(wasm.HeapI31), 0x01, 0x02, 0x04}),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedI31TableCompiles(t *testing.T) {
	source := rv64RootedI31TableModule(false)
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("private rooted i31 table was not admitted")
	}
	if AdmitGCRootedScalarSource(rv64RootedI31TableModule(true)) {
		t.Fatal("public rooted i31 table was admitted")
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
	if !plan.SetLiveMasks([]uint64{0}, 1, 0) {
		t.Fatal("install exact root liveness")
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
		t.Fatalf("code=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
	}
}

func rv64RootedI31PassiveElementModule() []byte {
	body := []byte{
		0x00,
		0x41, 0x00, 0x41, 0x00, 0x41, 0x02, 0xfc, 0x0c, 0x00, 0x00,
		0xfc, 0x0d, 0x00,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		0x41, 0x00, 0x25, 0x00, 0xfb, 0x1d,
		0x41, 0x01, 0x25, 0x00, 0xfb, 0x1d,
		0x0b,
	}
	elements := []byte{0x01, 0x05, byte(wasm.HeapI31), 0x02,
		0x41, 0x7e, 0xfb, 0x1c, 0x0b,
		0x41, 0x7d, 0xfb, 0x1c, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, []byte{0x01, byte(wasm.HeapI31), 0x01, 0x02, 0x02}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(9, elements),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedI31PassiveElementCompiles(t *testing.T) {
	source := rv64RootedI31PassiveElementModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("passive rooted i31 elements were not admitted")
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
	if !plan.SetLiveMasks([]uint64{0}, 1, 0) {
		t.Fatal("install exact root liveness")
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
		t.Fatalf("code=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
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

func rv64RootedAggregateTableInitializerModule() []byte {
	body := []byte{
		0x00,
		0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		// Core 3's table-with-initializer encoding. The expression constructs one
		// object which the table initialization semantics repeat into every slot.
		wasmtest.Section(4, []byte{
			0x01, 0x40, 0x00, 0x63, 0x00, 0x01, 0x03, 0x03,
			0x41, 0x37, 0xfb, 0x00, 0x00, 0x0b,
		}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rv64RootedAggregateSubtypeTableInitializerModule(nested bool, unrelated bool) []byte {
	types := [][]byte{
		{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x01},       // open struct supertype
		{0x4f, 0x01, 0x00, 0x5f, 0x01, 0x7f, 0x01}, // final struct subtype
		{0x5f, 0x01, 0x7e, 0x01},                   // unrelated i64 struct
	}
	tableType := uint32(0)
	init := []byte{0x41, 0x37, 0xfb, 0x00, 0x01}
	if unrelated {
		init = []byte{0x42, 0x37, 0xfb, 0x00, 0x02}
	}
	if nested {
		tableType = 3
		types = append(types, []byte{0x5f, 0x01, 0x63, 0x00, 0x01}) // holder of (ref null 0)
		init = append(init, 0xfb, 0x00, 0x03)
	}
	functionType := uint32(len(types))
	types = append(types, wasmtest.FuncType(nil, nil))
	body := []byte{0x00, 0x41, 0x01, 0xfb, 0x00, 0x01, 0x1a, 0x0b}
	table := []byte{0x01, 0x40, 0x00, 0x63}
	table = append(table, byte(tableType))
	table = append(table, 0x01, 0x02, 0x02)
	table = append(table, init...)
	table = append(table, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(functionType))),
		wasmtest.Section(4, table),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rv64RootedAggregateGlobalInitializerModule() []byte {
	body := []byte{0x00, 0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a, 0x41, 0x07, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(
			[]byte{
				0x64, 0x00, 0x00, // immutable (ref 0)
				0x41, 0x21, 0xfb, 0x00, 0x00, 0x0b, // struct.new 0 (33)
			},
			[]byte{
				0x63, 0x00, 0x00, // immutable (ref null 0)
				0x23, 0x00, 0x0b, // global.get 0
			},
		)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedAggregateGlobalInitializerCompiles(t *testing.T) {
	source := rv64RootedAggregateGlobalInitializerModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("aggregate global initializer was not admitted")
	}
	if len(m.Globals) != 2 {
		t.Fatalf("globals=%d want=2", len(m.Globals))
	}
	m.Globals[1].Init.BodyBytes = []byte{0x23, 0x01, 0x0b}
	if admitRV64GCRootedScalarModule(m) {
		t.Fatal("self-referential aggregate global initializer was admitted")
	}
	m.Globals[1].Init.BodyBytes = []byte{0x23, 0x00, 0x0b}
	m.Globals[0].Init.BodyBytes = []byte{0x23, 0x01, 0x0b}
	if admitRV64GCRootedScalarModule(m) {
		t.Fatal("forward aggregate global initializer was admitted")
	}
	m.Globals[0].Init.BodyBytes = []byte{0x41, 0x21, 0xfb, 0x00, 0x01, 0x0b}
	if admitRV64GCRootedScalarModule(m) {
		t.Fatal("aggregate global initializer with a function constructor was admitted")
	}
}

func rv64RootedSelfRecursiveStructModule() []byte {
	body := []byte{
		0x00,
		0x23, 0x00, 0x41, 0x29, 0xfb, 0x00, 0x00, 0x24, 0x00,
		0x23, 0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0x24, 0x00,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x02, 0x63, 0x00, 0x01, 0x7f, 0x00},
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedSelfRecursiveStructCompiles(t *testing.T) {
	source := rv64RootedSelfRecursiveStructModule()
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	field := m.Types[0].SubTypes[0].Comp.Fields[0].Storage().Val().Ref().Heap().Type()
	if !field.Rec || field.Index != 0 {
		t.Fatalf("recursive field=%+v", field)
	}
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("singleton self-recursive struct was not admitted")
	}
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata.Descs) < 1 || !metadata.Descs[0].HasRefs || len(metadata.Descs[0].Fields) != 2 {
		t.Fatalf("recursive struct metadata=%+v", metadata.Descs)
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
	compiled, err := CompileModuleWith(m, CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCStructHelpers: true, SyncHostSlots: 4, GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Code) == 0 || plan.SafepointCount() != 2 {
		t.Fatalf("code=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
	}
}

func rv64RootedMutuallyRecursiveStructModule() []byte {
	body := []byte{
		0x00,
		// B(null A) -> global B, then A(B) -> global A.
		0xd0, 0x00, 0xfb, 0x00, 0x01, 0x24, 0x01,
		0x23, 0x01, 0xfb, 0x00, 0x00, 0x24, 0x00,
		// Close the object cycle by setting B.a = A.
		0x23, 0x01, 0x23, 0x00, 0xfb, 0x05, 0x01, 0x00,
		// Observe A.b.a as a non-null reference.
		0x23, 0x00, 0xfb, 0x02, 0x00, 0x00, 0xfb, 0x02, 0x01, 0x00, 0xd1,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{
				0x4e, 0x02,
				0x5f, 0x01, 0x63, 0x01, 0x01, // A { mutable (ref null B) }
				0x5f, 0x01, 0x63, 0x00, 0x01, // B { mutable (ref null A) }
			},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec(
			[]byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b},
			[]byte{0x63, 0x01, 0x01, 0xd0, 0x01, 0x0b},
		)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedMutuallyRecursiveStructsCompile(t *testing.T) {
	source := rv64RootedMutuallyRecursiveStructModule()
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
		t.Fatal("mutually recursive fixture", err)
	}
	if len(m.Types) != 2 || len(m.Types[0].SubTypes) != 2 {
		t.Fatalf("recursive groups=%d members=%d", len(m.Types), len(m.Types[0].SubTypes))
	}
	directory, ok := newInternalReferenceTypeDirectory(m.Types)
	if !ok {
		t.Fatal("build flattened recursive type directory")
	}
	for index, want := range []struct {
		base, size uint32
	}{{0, 2}, {0, 2}, {2, 1}} {
		sub, base, size, found := directory.lookup(uint32(index))
		if !found || sub == nil || base != want.base || size != want.size {
			t.Fatalf("directory[%d]=%p/%d/%d/%t want base=%d size=%d", index, sub, base, size, found, want.base, want.size)
		}
	}
	if _, _, _, found := directory.lookup(3); found {
		t.Fatal("flattened recursive type directory accepted out-of-range index")
	}
	for member := range m.Types[0].SubTypes {
		heap := m.Types[0].SubTypes[member].Comp.Fields[0].Storage().Val().Ref().Heap().Type()
		if !heap.Rec || heap.Index != uint32(1-member) {
			t.Fatalf("member %d recursive field=%+v", member, heap)
		}
	}
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("mutually recursive structs were not admitted")
	}
	if err = AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata.Descs) < 2 || !metadata.Descs[0].HasRefs || !metadata.Descs[1].HasRefs {
		t.Fatalf("mutually recursive metadata=%+v", metadata.Descs)
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
	compiled, err := CompileModuleWith(m, CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCStructHelpers: true, SyncHostSlots: 4, GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Code) == 0 || plan.SafepointCount() != 2 {
		t.Fatalf("code=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
	}
}

func rv64RootedMutuallyRecursiveStructArrayModule() []byte {
	body := []byte{
		0x00,
		// B = array.new 1(null A, 1); A = struct.new 0(B).
		0xd0, 0x00, 0x41, 0x01, 0xfb, 0x06, 0x01, 0x24, 0x01,
		0x23, 0x01, 0xfb, 0x00, 0x00, 0x24, 0x00,
		// Close the cycle with B[0] = A, then observe A.b[0].
		0x23, 0x01, 0x41, 0x00, 0x23, 0x00, 0xfb, 0x0e, 0x01,
		0x23, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x41, 0x00, 0xfb, 0x0b, 0x01, 0xd1,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{
				0x4e, 0x02,
				0x5f, 0x01, 0x63, 0x01, 0x01,
				0x5e, 0x63, 0x00, 0x01,
			},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec(
			[]byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b},
			[]byte{0x63, 0x01, 0x01, 0xd0, 0x01, 0x0b},
		)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedMutuallyRecursiveStructArrayCompile(t *testing.T) {
	source := rv64RootedMutuallyRecursiveStructArrayModule()
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{MaxFunctionLocals: 256}); err != nil {
		t.Fatal("mutually recursive struct/array fixture", err)
	}
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("mutually recursive struct/array group was not admitted")
	}
	if err = AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata.Descs) < 2 || !metadata.Descs[0].HasRefs || !metadata.Descs[1].HasRefs {
		t.Fatalf("mutually recursive struct/array metadata=%+v", metadata.Descs)
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
	compiled, err := CompileModuleWith(m, CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCStructHelpers: true, GCArrayHelpers: true, SyncHostSlots: 4, GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Code) == 0 || plan.SafepointCount() != 2 {
		t.Fatalf("code=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
	}
}

func rv64RootedSelfRecursiveArrayModule() []byte {
	body := []byte{
		0x00,
		0xd0, 0x00, 0xfb, 0x08, 0x00, 0x01, 0x24, 0x00,
		0x23, 0x00, 0xfb, 0x08, 0x00, 0x01, 0x24, 0x00,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5e, 0x63, 0x00, 0x01},
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedSelfRecursiveArrayCompiles(t *testing.T) {
	source := rv64RootedSelfRecursiveArrayModule()
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("singleton self-recursive array was not admitted")
	}
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata.Descs) < 1 || !metadata.Descs[0].HasRefs {
		t.Fatalf("recursive array metadata=%+v", metadata.Descs)
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
	compiled, err := CompileModuleWith(m, CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCArrayHelpers: true, SyncHostSlots: 4, GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Code) == 0 || plan.SafepointCount() != 2 {
		t.Fatalf("code=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
	}
}

func rv64RootedExactAggregateStorageModule(array bool) []byte {
	outer := []byte{0x5f, 0x01, 0x64, 0x62, 0x00, 0x00}
	body := []byte{0x00,
		0x41, 0x29, 0xfb, 0x00, 0x00, 0xfb, 0x16, 0x62, 0x00, 0xfb, 0x00, 0x01, 0x24, 0x00,
		0x23, 0x00, 0xfb, 0x02, 0x01, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	if array {
		outer = []byte{0x5e, 0x64, 0x62, 0x00, 0x00}
		body = []byte{0x00,
			0x41, 0x29, 0xfb, 0x00, 0x00, 0xfb, 0x16, 0x62, 0x00, 0xfb, 0x08, 0x01, 0x01, 0x24, 0x00,
			0x23, 0x00, 0x41, 0x00, 0xfb, 0x0b, 0x01, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			outer,
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x01, 0x01, 0xd0, 0x01, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedExactAggregateStorageCompiles(t *testing.T) {
	for _, array := range []bool{false, true} {
		t.Run(fmt.Sprintf("array=%t", array), func(t *testing.T) {
			source := rv64RootedExactAggregateStorageModule(array)
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			storage := m.Types[1].SubTypes[0].Comp.Array.Storage()
			if !array {
				storage = m.Types[1].SubTypes[0].Comp.Fields[0].Storage()
			}
			field := storage.Val().Ref()
			if !field.Exact() || field.Heap().Type().Index != 0 {
				t.Fatalf("exact field=%+v", field)
			}
			if !admitRV64GCRootedScalarModule(m) {
				t.Fatal("exact aggregate field was not admitted")
			}
			if err := AdmitProductionSource(source); err != nil {
				t.Fatal(err)
			}
			metadata, err := frontend.BuildGCTypeMetadata(m)
			if err != nil {
				t.Fatal(err)
			}
			if len(metadata.Descs) < 2 || !metadata.Descs[1].HasRefs {
				t.Fatalf("exact field metadata=%+v", metadata.Descs)
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
			compiled, err := CompileModuleWith(m, CompileOptions{
				DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
				GCStructHelpers: true, GCArrayHelpers: array, SyncHostSlots: 4, GCFrameRoots: roots, GCFrameRootsSource: true,
				Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(compiled.Code) == 0 || plan.SafepointCount() != 2 {
				t.Fatalf("code=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
			}
		})
	}
}

func TestRV64RootedAggregateSubtypeTableInitializerCompiles(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprintf("nested=%t", nested), func(t *testing.T) {
			source := rv64RootedAggregateSubtypeTableInitializerModule(nested, false)
			if err := AdmitProductionSource(source); err != nil {
				decoded, decodeErr := wasm.DecodeModule(source)
				t.Fatalf("%v (root-wire=%t decode=%v decoded=%t)", err, admitRV64GCRootedScalarWire(source), decodeErr, decodeErr == nil && admitRV64GCRootedScalarModule(decoded))
			}
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			if !admitRV64GCRootedScalarModule(m) {
				t.Fatal("aggregate subtype table initializer was not admitted")
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
			if !plan.SetLiveMasks([]uint64{0}, 1, 0) {
				t.Fatal("install exact root liveness")
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
				t.Fatalf("code=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
			}
		})
	}
	for _, nested := range []bool{false, true} {
		if err := AdmitProductionSource(rv64RootedAggregateSubtypeTableInitializerModule(nested, true)); err == nil {
			t.Fatalf("unrelated aggregate constructor escaped subtype table admission (nested=%t)", nested)
		}
	}
}

func TestRV64RootedAggregateTableInitializerCompiles(t *testing.T) {
	source := rv64RootedAggregateTableInitializerModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("aggregate table initializer was not admitted")
	}
	candidate, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	candidate.Tables[0].Init.BodyBytes = []byte{0x42, 0x37, 0xfb, 0x00, 0x00, 0x0b}
	if admitRV64GCRootedScalarModule(candidate) {
		t.Fatal("wrong-width aggregate table initializer was admitted")
	}
	empty, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	empty.Tables[0].Type.Limits.Min = 0
	if admitRV64GCRootedScalarModule(empty) {
		t.Fatal("zero-sized allocating aggregate table initializer was admitted")
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
	if !plan.SetLiveMasks([]uint64{0}, 1, 0) {
		t.Fatal("install exact root liveness")
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
		t.Fatalf("code=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
	}
}

func rv64RootedAggregateGlobalTableInitializerModule() []byte {
	body := []byte{
		0x00,
		0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, []byte{
			0x01, 0x40, 0x00, 0x63, 0x00, 0x01, 0x02, 0x02,
			0x23, 0x00, 0xfb, 0x00, 0x00, 0x0b,
		}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x7f, 0x00, 0x41, 0x37, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rv64RootedAggregateImportedGlobalTableInitializerModule() []byte {
	body := []byte{
		0x00,
		0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "base", wasm.I32, false))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, []byte{
			0x01, 0x40, 0x00, 0x63, 0x00, 0x01, 0x02, 0x02,
			0x23, 0x00, 0xfb, 0x00, 0x00, 0x0b,
		}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rv64RootedAggregateReferenceGlobalTableInitializerModule() []byte {
	body := []byte{
		0x00,
		0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			[]byte{0x5f, 0x01, 0x63, 0x00, 0x01},
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(4, []byte{
			0x01, 0x40, 0x00, 0x63, 0x01, 0x01, 0x02, 0x02,
			0x23, 0x00, 0xfb, 0x00, 0x01, 0x0b,
		}),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x00, 0x00, 0xd0, 0x00, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedAggregateGlobalTableInitializerCompiles(t *testing.T) {
	source := rv64RootedAggregateGlobalTableInitializerModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("aggregate global table initializer was not admitted")
	}
	referenceSource := rv64RootedAggregateReferenceGlobalTableInitializerModule()
	if err := AdmitProductionSource(referenceSource); err != nil {
		t.Fatal(err)
	}
	reference, err := wasm.DecodeModule(referenceSource)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCRootedScalarModule(reference) {
		t.Fatal("aggregate reference-global table initializer was not admitted")
	}
	importedSource := rv64RootedAggregateImportedGlobalTableInitializerModule()
	if err := AdmitProductionSource(importedSource); err != nil {
		t.Fatal(err)
	}
	imported, err := wasm.DecodeModule(importedSource)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCRootedScalarModule(imported) {
		t.Fatal("aggregate imported-global table initializer was not admitted")
	}
	imported.Imports[0].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.I32, Mutable: true})
	if admitRV64GCRootedScalarModule(imported) {
		t.Fatal("mutable imported aggregate-constructor global was admitted")
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"mutable": func(candidate *wasm.Module) { candidate.Globals[0].Type.Mutable = true },
		"out-of-range": func(candidate *wasm.Module) {
			candidate.Tables[0].Init.BodyBytes = []byte{0x23, 0x01, 0xfb, 0x00, 0x00, 0x0b}
		},
		"wrong-width": func(candidate *wasm.Module) {
			candidate.Globals[0].Type.Type = wasm.I64
			candidate.Globals[0].Init.BodyBytes = []byte{0x42, 0x37, 0x0b}
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			mutate(candidate)
			if admitRV64GCRootedScalarModule(candidate) {
				t.Fatal("invalid aggregate global table initializer was admitted")
			}
		})
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
	if !plan.SetLiveMasks([]uint64{0}, 1, 0) {
		t.Fatal("install exact root liveness")
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
		t.Fatalf("code=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
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

func TestRV64RootedScalarTypeWireBoundsRecursiveGroups(t *testing.T) {
	function := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	valid := wasmtest.Vec(
		[]byte{
			0x4e, 0x02,
			0x5f, 0x01, 0x63, 0x01, 0x01,
			0x5f, 0x01, 0x63, 0x00, 0x01,
		},
		function,
	)
	if !admitRV64GCRootedScalarTypeWire(valid) {
		t.Fatal("bounded two-member recursive group was rejected")
	}
	for name, group := range map[string][]byte{
		"exact-member": {
			0x4e, 0x02,
			0x5f, 0x01, 0x63, 0x62, 0x01, 0x01,
			0x5f, 0x01, 0x63, 0x00, 0x01,
		},
		"out-of-group-forward": {
			0x4e, 0x02,
			0x5f, 0x01, 0x63, 0x02, 0x01,
			0x5f, 0x01, 0x63, 0x00, 0x01,
		},
		"empty": {0x4e, 0x00},
		"function-member": {
			0x4e, 0x02,
			0x60, 0x00, 0x00,
			0x60, 0x00, 0x00,
		},
		"aggregate-to-function": {
			0x4e, 0x02,
			0x5f, 0x01, 0x63, 0x01, 0x01,
			0x60, 0x00, 0x00,
		},
		"prefixed-member": {
			0x4e, 0x02,
			0x50, 0x00, 0x5f, 0x01, 0x63, 0x01, 0x01,
			0x5f, 0x01, 0x63, 0x00, 0x01,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if admitRV64GCRootedScalarTypeWire(wasmtest.Vec(group, function)) {
				t.Fatal("invalid recursive group escaped raw type admission")
			}
		})
	}
}

func TestRV64RootedScalarTypeWireAcceptsFlatAggregateSubtypes(t *testing.T) {
	body := []byte{0x00, 0x41, 0x07, 0xfb, 0x00, 0x01, 0x1a, 0x0b}
	module := func(types ...[]byte) []byte {
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(types...)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
			wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
		)
	}
	open := []byte{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x01}
	child := []byte{0x4f, 0x01, 0x00, 0x5f, 0x01, 0x7f, 0x01}
	function := wasmtest.FuncType(nil, nil)
	source := module(open, child, function)
	if err := AdmitProductionSource(source); err != nil {
		decoded, decodeErr := wasm.DecodeModule(source)
		t.Fatalf("flat aggregate subtype source rejected: %v (root-wire=%t decode=%v decoded=%t)", err, admitRV64GCRootedScalarWire(source), decodeErr, decodeErr == nil && admitRV64GCRootedScalarModule(decoded))
	}
	if m, err := wasm.DecodeModule(source); err != nil || !admitRV64GCRootedScalarModule(m) {
		t.Fatalf("flat aggregate subtype decoded admission=%t err=%v", err == nil && admitRV64GCRootedScalarModule(m), err)
	}
	for name, bad := range map[string][]byte{
		"future-super": {0x4f, 0x01, 0x02, 0x5f, 0x01, 0x7f, 0x01},
		"wrong-kind":   {0x4f, 0x01, 0x00, 0x5e, 0x7f, 0x01},
		"function":     {0x4f, 0x01, 0x00, 0x60, 0x00, 0x00},
	} {
		t.Run(name, func(t *testing.T) {
			if err := AdmitProductionSource(module(open, bad, function)); err == nil {
				t.Fatal("invalid flat aggregate subtype escaped wire admission")
			}
		})
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

func rv64RootedScalarI31LocalModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32}),
	}
	body := []byte{
		0x01, 0x01, 0x6c,
		0x41, 0x7f, 0xfb, 0x1c, 0x21, 0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00, 0x1a,
		0x20, 0x00, 0xfb, 0x1e,
		0x20, 0x00, 0xfb, 0x1d,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedScalarI31LocalCompiles(t *testing.T) {
	source := rv64RootedScalarI31LocalModule()
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("rooted i31 local module was not admitted")
	}
	if err = AdmitProductionSource(source); err != nil {
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
		t.Fatalf("code=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
	}
}

func rv64RootedScalarI31InternalCallModule() []byte {
	callee := []byte{
		0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00, 0x1a,
		0x20, 0x00,
		0x0b,
	}
	caller := []byte{
		0x00,
		0x41, 0x7f, 0xfb, 0x1c, 0x10, 0x00, 0xfb, 0x1e,
		0x41, 0x7f, 0xfb, 0x1c, 0x10, 0x00, 0xfb, 0x1d,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			[]byte{0x60, 0x01, 0x6c, 0x01, 0x6c},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(callee))), callee...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func TestRV64RootedScalarI31InternalCallCompiles(t *testing.T) {
	source := rv64RootedScalarI31InternalCallModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("rooted i31 internal call module was not admitted")
	}
	m.Exports[0].Index.Index = 0
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("public i31 function boundary was not admitted")
	}
	m.Exports[0].Index.Index = 1
	roots := shared.NewGCModuleFrameRootPlan(2)
	if !roots.MarkFunction(0) || !roots.MarkFunction(1) || !roots.ReserveFunctions(2) {
		t.Fatal("reserve exact root plans")
	}
	callee, ok := roots.BeginFunction(0)
	if !ok {
		t.Fatal("begin callee root plan")
	}
	*callee = shared.GCFrameRootPlan{
		Candidate: true, Exact: true, Conservative: true,
		Locals: []shared.GCFrameLocal{{Index: 0, Offset: shared.RV64CallFrameHeaderBytes}},
	}
	if !callee.SetLiveMasks([]uint64{1}, 1, 0) {
		t.Fatal("install callee root masks")
	}
	caller, ok := roots.BeginFunction(1)
	if !ok {
		t.Fatal("begin caller root plan")
	}
	*caller = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true, SafepointBase: 1}
	if !caller.SetLiveMasks([]uint64{0, 0}, 0, 2) {
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
	if len(compiled.Code) == 0 || callee.SafepointCount() != 1 || caller.SafepointCount() != 0 || caller.CallsiteCount() != 2 {
		t.Fatalf("code=%d callee-safepoints=%d caller-safepoints=%d callsites=%d", len(compiled.Code), callee.SafepointCount(), caller.SafepointCount(), caller.CallsiteCount())
	}
}

func rv64RootedScalarI31StructModule() []byte {
	body := []byte{
		0x01, 0x01, 0x64, 0x00,
		0x41, 0x7f, 0xfb, 0x1c, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x20, 0x00, 0x41, 0x7e, 0xfb, 0x1c, 0xfb, 0x05, 0x00, 0x00,
		0x41, 0x01, 0xfb, 0x1c, 0xfb, 0x00, 0x00, 0x1a,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0xfb, 0x1e,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0xfb, 0x1d,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x6c, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedScalarI31StructCompiles(t *testing.T) {
	source := rv64RootedScalarI31StructModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("rooted i31 struct module was not admitted")
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
	if !plan.SetLiveMasks([]uint64{1, 1}, 2, 0) {
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
	if len(compiled.Code) == 0 || plan.SafepointCount() != 2 {
		t.Fatalf("code=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
	}
}

func rv64RootedScalarI31ArrayModule() []byte {
	body := []byte{
		0x01, 0x01, 0x64, 0x00,
		0x41, 0x7f, 0xfb, 0x1c, 0x41, 0x02, 0xfb, 0x06, 0x00, 0x21, 0x00,
		0x20, 0x00, 0x41, 0x00, 0x41, 0x7e, 0xfb, 0x1c, 0xfb, 0x0e, 0x00,
		0x41, 0x01, 0xfb, 0x1c, 0x41, 0x01, 0xfb, 0x06, 0x00, 0x1a,
		0x20, 0x00, 0x41, 0x00, 0xfb, 0x0b, 0x00, 0xfb, 0x1e,
		0x20, 0x00, 0x41, 0x00, 0xfb, 0x0b, 0x00, 0xfb, 0x1d,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5e, 0x6c, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedScalarI31ArrayCompiles(t *testing.T) {
	source := rv64RootedScalarI31ArrayModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("rooted i31 array module was not admitted")
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
	if !plan.SetLiveMasks([]uint64{1, 1}, 2, 0) {
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
	if len(compiled.Code) == 0 || plan.SafepointCount() != 2 {
		t.Fatalf("code=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
	}
}

func rv64RootedScalarReferenceControlModule() []byte {
	body := []byte{
		0x01, 0x01, 0x64, 0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x20, 0x00, 0x20, 0x00, 0xd3,
		0x41, 0x07, 0xfb, 0x1c, 0x41, 0x07, 0xfb, 0x1c, 0xd3,
		0xd0, 0x6c, 0xd1,
		0x41, 0x09, 0xfb, 0x1c, 0xd4, 0xfb, 0x1e,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedScalarReferenceControlCompiles(t *testing.T) {
	source := rv64RootedScalarReferenceControlModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("rooted reference control module was not admitted")
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
		t.Fatalf("code=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
	}
}

func rv64RootedScalarI31TestCastModule() []byte {
	body := []byte{
		0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00, 0x1a,
		0x41, 0x07, 0xfb, 0x1c, 0xfb, 0x14, byte(wasm.HeapI31),
		0x41, 0x07, 0xfb, 0x1c, 0xfb, 0x14, byte(wasm.HeapStruct),
		0x41, 0x09, 0xfb, 0x1c, 0xfb, 0x16, byte(wasm.HeapI31), 0xfb, 0x1e,
		0xd0, byte(wasm.HeapI31), 0xfb, 0x17, byte(wasm.HeapI31), 0xd1,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedScalarI31TestCastCompiles(t *testing.T) {
	source := rv64RootedScalarI31TestCastModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("rooted i31 ref.test/ref.cast module was not admitted")
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
		t.Fatalf("code=%d safepoints=%d", len(compiled.Code), plan.SafepointCount())
	}
}
