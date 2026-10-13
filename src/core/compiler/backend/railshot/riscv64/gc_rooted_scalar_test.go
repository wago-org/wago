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
		wasmtest.Section(9, []byte{0x01, 0x05, 0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b}),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"active": func(candidate *wasm.Module) {
			candidate.Elements[0].Mode.Kind = wasm.ElemActive
			candidate.Elements[0].Mode.Offset = wasm.Expr{BodyBytes: []byte{0x41, 0x00, 0x0b}}
		},
		"declarative": func(candidate *wasm.Module) { candidate.Elements[0].Mode.Kind = wasm.ElemDeclarative },
		"wrong-null-heap": func(candidate *wasm.Module) {
			candidate.Elements[0].Kind.Exprs[0].BodyBytes = []byte{0xd0, 0x01, 0x0b}
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
