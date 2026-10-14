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
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
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

func rv64RootedLocalTailModule() []byte {
	callee := []byte{
		0x00,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x0b,
	}
	caller := []byte{
		0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00,
		0x12, 0x00,
		0x0b,
	}
	wrapper := []byte{0x00, 0x12, 0x01, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			[]byte{0x60, 0x01, 0x64, 0x00, 0x01, 0x7f},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 2))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(callee))), callee...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
			append(wasmtest.ULEB(uint32(len(wrapper))), wrapper...),
		)),
	)
}

func TestRV64RootedLocalTailCompiles(t *testing.T) {
	source := rv64RootedLocalTailModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	if !AdmitGCRootedScalarSource(source) {
		t.Fatal("rooted local tail source was not admitted")
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	roots := shared.NewGCModuleFrameRootPlan(3)
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
	if !caller.SetLiveMasks([]uint64{0}, 1, 0) {
		t.Fatal("install caller root masks")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := CompileModuleWith(m, CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		NativeTailCalls: true, GCStructHelpers: true, SyncHostSlots: 4,
		GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Code) == 0 || callee.SafepointCount() != 1 || caller.SafepointCount() != 1 || callee.CallsiteCount() != 0 || caller.CallsiteCount() != 0 {
		t.Fatalf("code=%d callee=%d/%d caller=%d/%d", len(compiled.Code), callee.SafepointCount(), callee.CallsiteCount(), caller.SafepointCount(), caller.CallsiteCount())
	}
	if roots.Function(2) != nil {
		t.Fatal("tail-only wrapper acquired a root plan")
	}
}

func TestRV64RootedLocalTailExactReferenceResultCompiles(t *testing.T) {
	callee := []byte{0x00, 0x20, 0x00, 0x0b}
	caller := []byte{0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0x12, 0x00, 0x0b}
	wrapper := []byte{0x00, 0x12, 0x01, 0x0b}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			[]byte{0x60, 0x01, 0x64, 0x00, 0x01, 0x64, 0x00},
			[]byte{0x60, 0x00, 0x01, 0x64, 0x00},
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("make", byte(wasm.ExternFunc), 2))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(callee))), callee...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
			append(wasmtest.ULEB(uint32(len(wrapper))), wrapper...),
		)),
	)
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	if !AdmitGCRootedScalarSource(source) {
		t.Fatal("rooted exact-reference result tail source was not admitted")
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	roots := shared.NewGCModuleFrameRootPlan(3)
	if !roots.MarkFunction(1) || !roots.ReserveFunctions(1) {
		t.Fatal("reserve exact root plan")
	}
	plan, ok := roots.BeginFunction(1)
	if !ok {
		t.Fatal("begin exact root plan")
	}
	*plan = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true}
	if !plan.SetLiveMasks([]uint64{0}, 1, 0) {
		t.Fatal("install exact root masks")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := CompileModuleWith(m, CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		NativeTailCalls: true, GCStructHelpers: true, SyncHostSlots: 4,
		GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Code) == 0 || plan.SafepointCount() != 1 || plan.CallsiteCount() != 0 || roots.Function(0) != nil || roots.Function(2) != nil {
		t.Fatalf("code=%d plan=%d/%d callee=%v wrapper=%v", len(compiled.Code), plan.SafepointCount(), plan.CallsiteCount(), roots.Function(0), roots.Function(2))
	}
}

func rv64RootedLocalTailSWARModule() []byte {
	callee := []byte{
		0x00,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x20, 0x01,
		0x0b,
	}
	caller := []byte{
		0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00,
		0x20, 0x00,
		0x12, 0x00,
		0x0b,
	}
	wrapper := []byte{0x00, 0x20, 0x00, 0x12, 0x01, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			[]byte{0x60, 0x02, 0x64, 0x00, 0x7b, 0x02, 0x7f, 0x7b},
			wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.I32, wasm.V128}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 2))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(callee))), callee...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
			append(wasmtest.ULEB(uint32(len(wrapper))), wrapper...),
		)),
	)
}

func TestRV64RootedLocalTailSWARCompiles(t *testing.T) {
	source := rv64RootedLocalTailSWARModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	if !AdmitGCRootedScalarSource(source) {
		t.Fatal("rooted local SWAR tail source was not admitted")
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	roots := shared.NewGCModuleFrameRootPlan(3)
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
	if !caller.SetLiveMasks([]uint64{0}, 1, 0) {
		t.Fatal("install caller root masks")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := CompileModuleWith(m, CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		NativeTailCalls: true, GCStructHelpers: true, SyncHostSlots: 6,
		GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Code) == 0 || callee.SafepointCount() != 1 || caller.SafepointCount() != 1 || callee.CallsiteCount() != 0 || caller.CallsiteCount() != 0 {
		t.Fatalf("code=%d callee=%d/%d caller=%d/%d", len(compiled.Code), callee.SafepointCount(), callee.CallsiteCount(), caller.SafepointCount(), caller.CallsiteCount())
	}
	if roots.Function(2) != nil {
		t.Fatal("tail-only SWAR wrapper acquired a root plan")
	}
}

func TestRV64RootedLocalTailRejectsDynamicTargets(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*wasm.Module)
	}{
		{
			name: "indirect",
			edit: func(m *wasm.Module) { m.Code[2].BodyBytes = []byte{0x00, 0x41, 0x00, 0x13, 0x02, 0x00, 0x0b} },
		},
		{
			name: "reference",
			edit: func(m *wasm.Module) { m.Code[2].BodyBytes = []byte{0x00, 0xd2, 0x01, 0x15, 0x02, 0x0b} },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := wasm.DecodeModule(rv64RootedLocalTailModule())
			if err != nil {
				t.Fatal(err)
			}
			tc.edit(m)
			if admitRV64GCRootedScalarModule(m) {
				t.Fatal("unsupported rooted tail target was admitted")
			}
		})
	}
}

func rv64RootedImportedNumericTailModule(vector, reference bool) []byte {
	importType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	if vector {
		importType = wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128})
	}
	if reference {
		importType = []byte{0x60, 0x01, 0x64, 0x00, 0x01, 0x64, 0x00}
	}
	imported := append(append(wasmtest.Name("env"), wasmtest.Name("tail")...), byte(wasm.ExternFunc), 0x01)
	allocator := []byte{0x00, 0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a, 0x0b}
	tail := []byte{0x00, 0x20, 0x00, 0x12, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			importType,
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(imported)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 2))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(allocator))), allocator...),
			append(wasmtest.ULEB(uint32(len(tail))), tail...),
		)),
	)
}

func TestRV64RootedImportedNumericTailCompiles(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(map[bool]string{false: "scalar", true: "swar"}[vector], func(t *testing.T) {
			source := rv64RootedImportedNumericTailModule(vector, false)
			if err := AdmitProductionSource(source); err != nil {
				t.Fatal(err)
			}
			if !AdmitGCRootedScalarSource(source) {
				t.Fatal("rooted imported numeric tail source was not admitted")
			}
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			roots := shared.NewGCModuleFrameRootPlan(2)
			if !roots.MarkFunction(0) || !roots.MarkFunction(1) || !roots.ReserveFunctions(2) {
				t.Fatal("reserve exact root plans")
			}
			allocator, ok := roots.BeginFunction(0)
			if !ok {
				t.Fatal("begin allocator root plan")
			}
			*allocator = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true}
			if !allocator.SetLiveMasks([]uint64{0}, 1, 0) {
				t.Fatal("install allocator root masks")
			}
			bridge, ok := roots.BeginFunction(1)
			if !ok {
				t.Fatal("begin imported-tail bridge plan")
			}
			*bridge = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true, SafepointBase: 1}
			if !bridge.SetLiveMasks(nil, 0, 0) {
				t.Fatal("install imported-tail bridge masks")
			}
			metadata, err := frontend.BuildGCTypeMetadata(m)
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := CompileModuleWith(m, CompileOptions{
				DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
				NativeTailCalls: true, GCStructHelpers: true, SyncHostSlots: 4,
				ImportBindings: []ImportBinding{{Dynamic: true, ImportIndex: 0}},
				GCFrameRoots:   roots, GCFrameRootsSource: true,
				Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(compiled.Code) == 0 || compiled.NativeTail == nil || len(compiled.NativeTail.Plans) != 1 || compiled.NativeTail.Plans[0].Kind != encoder.NativeTailDirectImport || allocator.SafepointCount() != 1 || bridge.SafepointCount() != 0 || bridge.CallsiteCount() != 0 {
				t.Fatalf("code=%d tail=%+v allocator=%d bridge=%d/%d", len(compiled.Code), compiled.NativeTail, allocator.SafepointCount(), bridge.SafepointCount(), bridge.CallsiteCount())
			}
		})
	}
}

func TestRV64RootedImportedReferenceTailRejected(t *testing.T) {
	source := rv64RootedImportedNumericTailModule(false, true)
	if AdmitGCRootedScalarSource(source) {
		t.Fatal("rooted imported collector-reference tail was admitted")
	}
}

func rv64RootedIndirectNumericTailModule(vector, reference bool) []byte {
	tailType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	if vector {
		tailType = wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128})
	}
	if reference {
		tailType = []byte{0x60, 0x01, 0x64, 0x00, 0x01, 0x64, 0x00}
	}
	target := []byte{0x20, 0x00, 0x0b}
	allocator := []byte{0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a, 0x0b}
	caller := []byte{0x20, 0x00, 0x41, 0x00, 0x13, 0x01, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			tailType,
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(1))),
		wasmtest.Section(4, []byte{0x01, 0x70, 0x01, 0x01, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 2))),
		wasmtest.Section(9, []byte{0x01, 0x00, 0x41, 0x00, 0x0b, 0x01, 0x00}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(target), wasmtest.Code(allocator), wasmtest.Code(caller))),
	)
}

func TestRV64RootedIndirectNumericTailCompiles(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(map[bool]string{false: "scalar", true: "swar"}[vector], func(t *testing.T) {
			source := rv64RootedIndirectNumericTailModule(vector, false)
			if err := AdmitProductionSource(source); err != nil {
				t.Fatal(err)
			}
			if !AdmitGCRootedScalarSource(source) {
				t.Fatal("rooted indirect numeric tail source was not admitted")
			}
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			roots := shared.NewGCModuleFrameRootPlan(3)
			if !roots.MarkFunction(1) || !roots.MarkFunction(2) || !roots.ReserveFunctions(2) {
				t.Fatal("reserve exact root plans")
			}
			allocator, ok := roots.BeginFunction(1)
			if !ok {
				t.Fatal("begin allocator root plan")
			}
			*allocator = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true}
			if !allocator.SetLiveMasks([]uint64{0}, 1, 0) {
				t.Fatal("install allocator root masks")
			}
			bridge, ok := roots.BeginFunction(2)
			if !ok {
				t.Fatal("begin indirect-tail bridge plan")
			}
			*bridge = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true, SafepointBase: 1}
			if !bridge.SetLiveMasks(nil, 0, 0) {
				t.Fatal("install indirect-tail bridge masks")
			}
			metadata, err := frontend.BuildGCTypeMetadata(m)
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := CompileModuleWith(m, CompileOptions{
				DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
				NativeTailCalls: true, GCStructHelpers: true, SyncHostSlots: 4,
				GCFrameRoots: roots, GCFrameRootsSource: true,
				Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(compiled.Code) == 0 || compiled.NativeTail == nil || len(compiled.NativeTail.Plans) != 1 || compiled.NativeTail.Plans[0].Kind != encoder.NativeTailIndirect || allocator.SafepointCount() != 1 || bridge.SafepointCount() != 0 || bridge.CallsiteCount() != 0 {
				t.Fatalf("code=%d tail=%+v allocator=%d bridge=%d/%d", len(compiled.Code), compiled.NativeTail, allocator.SafepointCount(), bridge.SafepointCount(), bridge.CallsiteCount())
			}
		})
	}
}

func TestRV64RootedIndirectReferenceTailRejected(t *testing.T) {
	if AdmitGCRootedScalarSource(rv64RootedIndirectNumericTailModule(false, true)) {
		t.Fatal("rooted indirect collector-reference tail was admitted")
	}
}

func TestRV64RootedReturnCallRefRemainsRejected(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(9, []byte{0x01, 0x03, 0x00, 0x01, 0x00}),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0xd2, 0x00, 0x15, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a, 0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = wasm.ValidateModule(m); err != nil {
		t.Fatal("return_call_ref rejection fixture", err)
	}
	if AdmitGCRootedScalarSource(source) {
		t.Fatal("rooted return_call_ref was admitted before its retained reference-owner boundary")
	}
}

func TestRV64RootedAbstractReferenceExportAdmitted(t *testing.T) {
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
	if err := AdmitProductionSource(source); err != nil {
		t.Fatalf("mixed-representation abstract reference export was not admitted: %v", err)
	}
}

func rv64RootedExactReferenceBoundaryModule() []byte {
	exact := []byte{0x63, 0x62, 0x00}
	exactEcho := append([]byte{0x60, 0x01}, exact...)
	exactEcho = append(exactEcho, 0x01)
	exactEcho = append(exactEcho, exact...)
	exactMake := append([]byte{0x60, 0x00, 0x01}, exact...)
	exactRead := append([]byte{0x60, 0x01}, exact...)
	exactRead = append(exactRead, 0x01, 0x7f)
	functionImport := append(append(wasmtest.Name("env"), wasmtest.Name("echo")...), byte(wasm.ExternFunc))
	functionImport = append(functionImport, wasmtest.ULEB(2)...)
	bodies := [][]byte{
		{0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0xfb, 0x16, 0x62, 0x00, 0x0b},
		{0x00, 0x41, 0x2b, 0xfb, 0x00, 0x01, 0x0b},
		{0x00, 0x20, 0x00, 0x0b},
		{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b},
		{0x00, 0x20, 0x00, 0xd4, 0xfb, 0x02, 0x00, 0x00, 0x0b},
	}
	codes := make([][]byte, len(bodies))
	for i, body := range bodies {
		codes[i] = append(wasmtest.ULEB(uint32(len(body))), body...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x01},
			[]byte{0x4f, 0x01, 0x00, 0x5f, 0x01, 0x7f, 0x01},
			exactEcho,
			exactMake,
			exactRead,
			[]byte{0x60, 0x00, 0x01, 0x64, 0x01},
		)),
		wasmtest.Section(2, wasmtest.Vec(functionImport)),
		wasmtest.Section(3, wasmtest.Vec(
			wasmtest.ULEB(3), wasmtest.ULEB(5), wasmtest.ULEB(2), wasmtest.ULEB(2), wasmtest.ULEB(4),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("make_base", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("make_sub", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("echo_local", byte(wasm.ExternFunc), 3),
			wasmtest.ExportEntry("echo_host", byte(wasm.ExternFunc), 4),
			wasmtest.ExportEntry("read", byte(wasm.ExternFunc), 5),
		)),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
}

func TestRV64RootedExactReferenceBoundaryAdmitted(t *testing.T) {
	source := rv64RootedExactReferenceBoundaryModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("exact indexed collector-reference boundary was not admitted")
	}
	ref := m.Types[2].SubTypes[0].Comp.Params[0].Ref()
	if !ref.Nullable() || !ref.Exact() || ref.Heap().Kind() != wasm.HeapTypeIndex || ref.Heap().Type().Index != 0 {
		t.Fatalf("exact boundary parameter=%v", m.Types[2].SubTypes[0].Comp.Params[0])
	}
	m.Types[2].SubTypes[0].Comp.Params[0] = wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapAny), true))
	if admitRV64GCRootedScalarModule(m) {
		t.Fatal("exact abstract collector-reference boundary was admitted")
	}
	exactAbstract := wasmtest.Vec([]byte{0x60, 0x01, 0x63, 0x62, byte(wasm.HeapAny), 0x00})
	if admitRV64GCRootedScalarTypeWire(exactAbstract) {
		t.Fatal("exact abstract collector-reference wire type was admitted")
	}
}

func rv64RootedExactArrayBoundaryModule() []byte {
	exact := []byte{0x63, 0x62, 0x00}
	exactEcho := append([]byte{0x60, 0x01}, exact...)
	exactEcho = append(exactEcho, 0x01)
	exactEcho = append(exactEcho, exact...)
	exactMake := append([]byte{0x60, 0x00, 0x01}, exact...)
	exactRead := append([]byte{0x60, 0x01}, exact...)
	exactRead = append(exactRead, 0x01, 0x7f)
	functionImport := append(append(wasmtest.Name("env"), wasmtest.Name("echo")...), byte(wasm.ExternFunc))
	functionImport = append(functionImport, wasmtest.ULEB(1)...)
	bodies := [][]byte{
		{0x00, 0x41, 0x2a, 0x41, 0x01, 0xfb, 0x06, 0x00, 0xfb, 0x16, 0x62, 0x00, 0x0b},
		{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b},
		{0x00, 0x20, 0x00, 0xd4, 0x41, 0x00, 0xfb, 0x0b, 0x00, 0x0b},
	}
	codes := make([][]byte, len(bodies))
	for i, body := range bodies {
		codes[i] = append(wasmtest.ULEB(uint32(len(body))), body...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5e, 0x7f, 0x01},
			exactEcho,
			exactMake,
			exactRead,
		)),
		wasmtest.Section(2, wasmtest.Vec(functionImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(1), wasmtest.ULEB(3))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("make", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("echo_host", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("read", byte(wasm.ExternFunc), 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
}

func TestRV64RootedExactArrayBoundaryAdmitted(t *testing.T) {
	source := rv64RootedExactArrayBoundaryModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("exact indexed array boundary was not admitted")
	}
}

func rv64RootedSIMDReferenceBoundaryModule(importVector bool) []byte {
	ref0 := []byte{0x63, 0x00}
	vectorIdentity := wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128})
	run := []byte{0x60, 0x02, 0x7b}
	run = append(run, ref0...)
	run = append(run, 0x02, 0x7b)
	run = append(run, ref0...)
	callIndex := uint32(0)
	functionTypes := [][]byte{wasmtest.ULEB(2)}
	// Keep the reference below the vector call operands, then discard both call
	// results and rebuild the public result tuple from canonical local homes.
	bodies := [][]byte{{0x00, 0x20, 0x01, 0x20, 0x00, 0x10, byte(callIndex), 0x1a, 0x1a, 0x20, 0x00, 0x20, 0x01, 0x0b}}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(
		[]byte{0x5f, 0x01, 0x7f, 0x01},
		vectorIdentity,
		run,
	))}
	if importVector {
		entry := append(append(wasmtest.Name("env"), wasmtest.Name("vector")...), byte(wasm.ExternFunc))
		entry = append(entry, wasmtest.ULEB(1)...)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entry)))
	} else {
		functionTypes = append([][]byte{wasmtest.ULEB(1)}, functionTypes...)
		bodies = append([][]byte{{0x00, 0x20, 0x00, 0x0b}}, bodies...)
	}
	codes := make([][]byte, len(bodies))
	for i, body := range bodies {
		codes[i] = append(wasmtest.ULEB(uint32(len(body))), body...)
	}
	sections = append(sections,
		wasmtest.Section(3, wasmtest.Vec(functionTypes...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
	return wasmtest.Module(sections...)
}

func TestRV64RootedSIMDReferenceBoundaryAdmitted(t *testing.T) {
	for _, imported := range []bool{false, true} {
		t.Run(fmt.Sprintf("imported=%t", imported), func(t *testing.T) {
			source := rv64RootedSIMDReferenceBoundaryModule(imported)
			if err := AdmitProductionSource(source); err != nil {
				t.Fatal(err)
			}
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			if !admitRV64GCRootedScalarModule(m) {
				t.Fatal("mixed SIMD/collector-reference boundary was not admitted")
			}
			metadata, err := frontend.BuildGCTypeMetadata(m)
			if err != nil {
				t.Fatal(err)
			}
			runIndex := 0
			if !imported {
				runIndex = 1
			}
			roots := shared.NewGCModuleFrameRootPlan(len(m.Code))
			if !roots.MarkFunction(runIndex) {
				t.Fatal("mark mixed SIMD/collector-reference root functions")
			}
			if !roots.ReserveFunctions(len(m.Code)) {
				t.Fatal("reserve mixed SIMD/collector-reference root plans")
			}
			plans := make([]*shared.GCFrameRootPlan, len(m.Code))
			for _, i := range []int{runIndex} {
				plan, ok := roots.BeginFunction(i)
				if !ok {
					t.Fatalf("begin function %d root plan", i)
				}
				*plan = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true}
				plans[i] = plan
			}
			plans[runIndex].Locals = []shared.GCFrameLocal{{Index: 1, Offset: shared.RV64CallFrameHeaderBytes + 16}}
			if !plans[runIndex].SetLiveMasks([]uint64{1}, 0, 1) {
				t.Fatal("install mixed SIMD call root mask")
			}
			if want, ok := rv64GCFrameLocals(m, runIndex); !ok || fmt.Sprint(want) != fmt.Sprint(plans[runIndex].Locals) {
				t.Fatalf("mixed SIMD run root locals=%v want=%v ok=%t", plans[runIndex].Locals, want, ok)
			}
			opts := CompileOptions{
				DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
				SyncHostSlots: 4, GCFrameRoots: roots, GCFrameRootsSource: true,
				Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
			}
			if imported {
				opts.ImportBindings = hostImportBindings(1)
			}
			compiled, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(compiled.Code) == 0 || plans[runIndex].CallsiteCount() != 1 {
				t.Fatalf("mixed SIMD code=%d run-callsites=%d", len(compiled.Code), plans[runIndex].CallsiteCount())
			}
			site, ok := plans[runIndex].Callsite(0)
			if !ok || site.StackAdjust() != 0 || fmt.Sprint(site.Offsets()) != fmt.Sprint([]uint32{shared.RV64CallFrameHeaderBytes + 16, shared.RV64CallFrameHeaderBytes + 32}) {
				t.Fatalf("mixed SIMD callsite=%+v ok=%t offsets=%v", site, ok, site.Offsets())
			}
		})
	}
}

func rv64RootedHybridSIMDGCModule() []byte {
	vector := []byte{0x00, 0x10, 0x01, 0x1a, 0x20, 0x00, 0x0b}
	collect := []byte{0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.V128}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("vector", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("collect", byte(wasm.ExternFunc), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(vector))), vector...),
			append(wasmtest.ULEB(uint32(len(collect))), collect...),
		)),
	)
}

func TestRV64RootedHybridSIMDGCFunctionsCompile(t *testing.T) {
	source := rv64RootedHybridSIMDGCModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if vector, err := functionUsesSWAR(m, 0); err != nil || !vector {
		t.Fatalf("vector selector=%t err=%v", vector, err)
	}
	if vector, err := functionUsesSWAR(m, 1); err != nil || vector {
		t.Fatalf("GC selector=%t err=%v", vector, err)
	}
	roots := shared.NewGCModuleFrameRootPlan(2)
	if !roots.MarkFunction(0) || !roots.MarkFunction(1) || !roots.ReserveFunctions(2) {
		t.Fatal("reserve hybrid roots")
	}
	vectorPlan, ok := roots.BeginFunction(0)
	if !ok {
		t.Fatal("begin hybrid vector roots")
	}
	*vectorPlan = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true}
	if !vectorPlan.SetLiveMasks([]uint64{0}, 0, 1) {
		t.Fatal("install hybrid vector call mask")
	}
	plan, ok := roots.BeginFunction(1)
	if !ok {
		t.Fatal("begin hybrid GC roots")
	}
	*plan = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true}
	if !plan.SetLiveMasks([]uint64{0}, 1, 0) {
		t.Fatal("install hybrid allocation mask")
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
	if len(compiled.Code) == 0 || vectorPlan.CallsiteCount() != 1 || plan.SafepointCount() != 1 || plan.CallsiteCount() != 0 {
		t.Fatalf("hybrid code=%d vector-callsites=%d safepoints=%d GC-callsites=%d", len(compiled.Code), vectorPlan.CallsiteCount(), plan.SafepointCount(), plan.CallsiteCount())
	}
}

func rv64RootedHybridVectorCallFlowModule() []byte {
	producer := append([]byte{0x00, 0xfd, 0x0c}, make([]byte, 16)...)
	producer = append(producer, 0x0b)
	collect := []byte{0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	// This function has a scalar signature, no vector locals, and no 0xfd
	// instruction. Its first direct call nevertheless puts a V128 on the
	// operand stack, so it must use the padded SWAR frame before calling the
	// integer/GC function through the opposite hybrid boundary.
	facade := []byte{0x00, 0x10, 0x00, 0x1a, 0x10, 0x01, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.V128}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 2))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(producer))), producer...),
			append(wasmtest.ULEB(uint32(len(collect))), collect...),
			append(wasmtest.ULEB(uint32(len(facade))), facade...),
		)),
	)
}

func TestRV64RootedHybridVectorCallFlowUsesSWARFrame(t *testing.T) {
	source := rv64RootedHybridVectorCallFlowModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	for function, want := range []bool{true, false, true} {
		got, err := functionUsesSWAR(m, function)
		if err != nil || got != want {
			t.Fatalf("function %d selector=%t want=%t err=%v", function, got, want, err)
		}
	}
	roots := shared.NewGCModuleFrameRootPlan(3)
	if !roots.MarkFunction(1) || !roots.MarkFunction(2) || !roots.ReserveFunctions(2) {
		t.Fatal("reserve vector call-flow roots")
	}
	plans := make([]*shared.GCFrameRootPlan, 3)
	for function := 1; function < len(plans); function++ {
		plan, ok := roots.BeginFunction(function)
		if !ok {
			t.Fatalf("begin vector call-flow roots %d", function)
		}
		*plan = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true}
		plans[function] = plan
	}
	if !plans[1].SetLiveMasks([]uint64{0}, 1, 0) || !plans[2].SetLiveMasks([]uint64{0, 0}, 0, 2) {
		t.Fatal("install vector call-flow masks")
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
	if len(compiled.Code) == 0 || plans[1].SafepointCount() != 1 || plans[2].CallsiteCount() != 2 {
		t.Fatalf("vector call-flow code=%d collector-safepoints=%d facade-calls=%d", len(compiled.Code), plans[1].SafepointCount(), plans[2].CallsiteCount())
	}
}

func rv64RootedSIMDStructFunctionModule() []byte {
	body := []byte{
		0x03, 0x01, 0x63, 0x01, 0x03, 0x7f, 0x01, 0x7e,
		0x20, 0x00,
		0xfb, 0x01, 0x00,
		0x20, 0x01,
		0x20, 0x02,
		0x41, 0x09,
		0xfb, 0x00, 0x01,
		0x22, 0x03,
		0xfb, 0x02, 0x01, 0x00,
		0x21, 0x04,
		0x20, 0x03,
		0xfb, 0x02, 0x01, 0x01,
		0x21, 0x07,
		0x20, 0x03,
		0x41, 0x0b,
		0xfb, 0x05, 0x01, 0x02,
		0x20, 0x03,
		0xfb, 0x02, 0x01, 0x02,
		0x21, 0x05,
		0xfb, 0x02, 0x00, 0x00,
		0x21, 0x06,
		0x20, 0x04,
		0x20, 0x07,
		0x20, 0x05,
		0x20, 0x06,
		0x41, 0x7f,
		0xfb, 0x1c,
		0xfb, 0x1d,
		0x41, 0x7f,
		0xfb, 0x1c,
		0xfb, 0x1e,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			[]byte{0x5f, 0x03, 0x7f, 0x00, 0x7e, 0x00, 0x7f, 0x01},
			wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.I32, wasm.I64}, []wasm.ValType{wasm.V128, wasm.I32, wasm.I64, wasm.I32, wasm.I32, wasm.I32, wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedSIMDStructAllocationAndGetCompile(t *testing.T) {
	source := rv64RootedSIMDStructFunctionModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if vector, err := functionUsesSWAR(m, 0); err != nil || !vector {
		t.Fatalf("SIMD/GC selector=%t err=%v", vector, err)
	}
	roots := shared.NewGCModuleFrameRootPlan(1)
	if !roots.MarkFunction(0) || !roots.ReserveFunctions(1) {
		t.Fatal("reserve SIMD/GC roots")
	}
	plan, ok := roots.BeginFunction(0)
	if !ok {
		t.Fatal("begin SIMD/GC roots")
	}
	*plan = shared.GCFrameRootPlan{
		Candidate: true, Exact: true, Conservative: true,
		Locals: []shared.GCFrameLocal{{Index: 3, Offset: shared.RV64CallFrameHeaderBytes + 48}},
	}
	if !plan.SetLiveMasks([]uint64{1, 1}, 2, 0) {
		t.Fatal("install SIMD/GC allocation mask")
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
	wantSafepoints := [][]uint32{
		{shared.RV64CallFrameHeaderBytes + 48},
		{shared.RV64CallFrameHeaderBytes + 48, shared.RV64CallFrameHeaderBytes + 16*8 + 16},
	}
	var safepoints [][]uint32
	if !plan.VisitSafepoints(func(_ int, offsets []uint32) bool {
		safepoints = append(safepoints, append([]uint32(nil), offsets...))
		return true
	}) {
		t.Fatal("visit SIMD/GC safepoints")
	}
	if len(compiled.Code) == 0 || plan.SafepointCount() != 2 || plan.CallsiteCount() != 0 || fmt.Sprint(safepoints) != fmt.Sprint(wantSafepoints) {
		t.Fatalf("SIMD/GC code=%d safepoints=%v want=%v callsites=%d", len(compiled.Code), safepoints, wantSafepoints, plan.CallsiteCount())
	}
}

func rv64RootedSIMDLargeStructModule() []byte {
	const fieldCount = 129
	structure := append([]byte{0x5f}, wasmtest.ULEB(fieldCount)...)
	for range fieldCount {
		structure = append(structure, 0x7f, 0x00)
	}
	body := []byte{0x01, 0x01, 0x7b}
	for range fieldCount {
		body = append(body, 0x41, 0x00)
	}
	body = append(body,
		0xfb, 0x00, 0x00,
		0xfb, 0x02, 0x00, 0x80, 0x01,
		0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			structure,
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedSIMDLargeStructCompiles(t *testing.T) {
	source := rv64RootedSIMDLargeStructModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if vector, err := functionUsesSWAR(m, 0); err != nil || !vector {
		t.Fatalf("large SIMD/GC selector=%t err=%v", vector, err)
	}
	roots := shared.NewGCModuleFrameRootPlan(1)
	if !roots.MarkFunction(0) || !roots.ReserveFunctions(1) {
		t.Fatal("reserve large SIMD/GC roots")
	}
	plan, ok := roots.BeginFunction(0)
	if !ok {
		t.Fatal("begin large SIMD/GC roots")
	}
	*plan = shared.GCFrameRootPlan{Candidate: true, Exact: true, Conservative: true}
	if !plan.SetLiveMasks([]uint64{0}, 1, 0) {
		t.Fatal("install large SIMD/GC allocation mask")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := CompileModuleWith(m, CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCStructHelpers: true, SyncHostSlots: 130, GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Code) == 0 || plan.SafepointCount() != 1 || plan.CallsiteCount() != 0 {
		t.Fatalf("large SIMD/GC code=%d safepoints=%d callsites=%d", len(compiled.Code), plan.SafepointCount(), plan.CallsiteCount())
	}
}

func rv64RootedSIMDArrayFunctionModule() []byte {
	body := []byte{
		0x02, 0x03, 0x63, 0x00, 0x01, 0x63, 0x01,
		0x20, 0x00,
		0x20, 0x01,
		0x41, 0x03,
		0xfb, 0x06, 0x00,
		0x22, 0x02,
		0x41, 0x01,
		0xfb, 0x0b, 0x00,
		0x20, 0x02,
		0x41, 0x01,
		0x41, 0x0b,
		0xfb, 0x0e, 0x00,
		0x20, 0x02,
		0x41, 0x01,
		0xfb, 0x0b, 0x00,
		0x20, 0x02,
		0xfb, 0x0f,
		0x41, 0x02,
		0xfb, 0x07, 0x00,
		0xfb, 0x0f,
		0x41, 0x04,
		0x41, 0x05,
		0x41, 0x06,
		0x41, 0x07,
		0xfb, 0x08, 0x00, 0x04,
		0x22, 0x03,
		0x41, 0x03,
		0xfb, 0x0b, 0x00,
		0x20, 0x02,
		0x41, 0x00,
		0x20, 0x03,
		0x41, 0x01,
		0x41, 0x02,
		0xfb, 0x11, 0x00, 0x00,
		0x20, 0x02,
		0x41, 0x01,
		0xfb, 0x0b, 0x00,
		0x20, 0x02,
		0x41, 0x00,
		0x41, 0x07,
		0x41, 0x03,
		0xfb, 0x10, 0x00,
		0x20, 0x02,
		0x41, 0x02,
		0xfb, 0x0b, 0x00,
		0x20, 0x02,
		0x20, 0x02,
		0xd3,
		0xd0, 0x00,
		0xd1,
		0x20, 0x02,
		0xfb, 0x14, 0x00,
		0x20, 0x02,
		0xfb, 0x16, 0x00,
		0xfb, 0x0f,
		0xd0, 0x00,
		0xfb, 0x15, 0x00,
		0x41, 0x00,
		0x41, 0x03,
		0xfb, 0x09, 0x00, 0x00,
		0x21, 0x04,
		0x20, 0x04,
		0x41, 0x00,
		0xfb, 0x0b, 0x00,
		0x20, 0x04,
		0x41, 0x01,
		0x41, 0x0c,
		0x41, 0x01,
		0xfb, 0x12, 0x00, 0x00,
		0x20, 0x04,
		0x41, 0x01,
		0xfb, 0x0b, 0x00,
		0x41, 0x08,
		0x74,
		0x72,
		0x41, 0x00,
		0x41, 0x02,
		0xfb, 0x0a, 0x01, 0x00,
		0x22, 0x05,
		0x41, 0x00,
		0xfb, 0x0b, 0x01,
		0xfb, 0x1e,
		0x20, 0x05,
		0x41, 0x00,
		0x41, 0x01,
		0x41, 0x01,
		0xfb, 0x13, 0x01, 0x00,
		0x20, 0x05,
		0x41, 0x00,
		0xfb, 0x0b, 0x01,
		0xfb, 0x1e,
		0x02, 0x02,
		0x20, 0x02,
		0xfb, 0x18, 0x01, 0x00, 0x00, 0x00,
		0xfb, 0x16, 0x00,
		0x0b,
		0xfb, 0x0f,
		0x02, 0x03,
		0xd0, 0x00,
		0xfb, 0x19, 0x01, 0x00, 0x00, 0x00,
		0x0b,
		0xd1,
		0x02, 0x04,
		0x41, 0x0b,
		0x20, 0x02,
		0xfb, 0x18, 0x01, 0x00, 0x6e, 0x00,
		0x1a,
		0x1a,
		0x41, 0x0c,
		0x20, 0x02,
		0xfb, 0x16, 0x00,
		0x0b,
		0x1a,
		0x02, 0x05,
		0x41, 0x15,
		0x20, 0x02,
		0xfb, 0x18, 0x03, 0x00, 0x6e, 0x6b,
		0x1a,
		0x1a,
		0x41, 0x16,
		0xd0, 0x6b,
		0x0b,
		0x1a,
		0x02, 0x06,
		0x41, 0x1f,
		0x20, 0x02,
		0xfb, 0x19, 0x03, 0x00, 0x6e, 0x6b,
		0x1a,
		0x1a,
		0x41, 0x20,
		0x20, 0x02,
		0xfb, 0x16, 0x00,
		0x0b,
		0x1a,
		0x02, 0x07,
		0x41, 0x29,
		0x20, 0x02,
		0xfb, 0x19, 0x01, 0x00, 0x6e, 0x00,
		0x1a,
		0x1a,
		0x41, 0x2a,
		0xd0, 0x6e,
		0x0b,
		0x1a,
		0x02, 0x04,
		0x41, 0x00,
		0x20, 0x02,
		0xfb, 0x16, 0x00,
		0x0c, 0x00,
		0x00,
		0xfb, 0x18, 0x01, 0x00, 0x6e, 0x00,
		0xfb, 0x16, 0x00,
		0x0b,
		0x1a,
		0x1a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5e, 0x7f, 0x01},
			[]byte{0x5e, byte(wasm.HeapI31), 0x01},
			[]byte{0x60, 0x00, 0x01, 0x64, 0x00},
			[]byte{0x60, 0x00, 0x01, 0x63, 0x00},
			[]byte{0x60, 0x00, 0x02, 0x7f, 0x64, 0x00},
			[]byte{0x60, 0x00, 0x02, 0x7f, 0x6b},
			[]byte{0x60, 0x00, 0x02, 0x7f, 0x64, 0x6e},
			[]byte{0x60, 0x00, 0x02, 0x7f, 0x6e},
			wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.I32}, []wasm.ValType{wasm.V128, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(8))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(9, []byte{0x01, 0x05, byte(wasm.HeapI31), 0x02, 0x41, 0x7e, 0xfb, 0x1c, 0x0b, 0x41, 0x7d, 0xfb, 0x1c, 0x0b}),
		wasmtest.Section(12, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
		wasmtest.Section(11, wasmtest.Vec([]byte{0x01, 0x10, 0x01, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00})),
	)
}

func TestRV64RootedSIMDArrayOperationsCompile(t *testing.T) {
	source := rv64RootedSIMDArrayFunctionModule()
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	if vector, err := functionUsesSWAR(m, 0); err != nil || !vector {
		t.Fatalf("SIMD/array selector=%t err=%v", vector, err)
	}
	roots := shared.NewGCModuleFrameRootPlan(1)
	if !roots.MarkFunction(0) || !roots.ReserveFunctions(1) {
		t.Fatal("reserve SIMD/array roots")
	}
	plan, ok := roots.BeginFunction(0)
	if !ok {
		t.Fatal("begin SIMD/array roots")
	}
	*plan = shared.GCFrameRootPlan{
		Candidate: true, Exact: true, Conservative: true,
		Locals: []shared.GCFrameLocal{
			{Index: 2, Offset: shared.RV64CallFrameHeaderBytes + 32},
			{Index: 3, Offset: shared.RV64CallFrameHeaderBytes + 48},
			{Index: 4, Offset: shared.RV64CallFrameHeaderBytes + 64},
			{Index: 5, Offset: shared.RV64CallFrameHeaderBytes + 80},
		},
	}
	if !plan.SetLiveMasks([]uint64{15, 15, 15, 15, 15}, 5, 0) {
		t.Fatal("install SIMD/array allocation masks")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := CompileModuleWith(m, CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCStructHelpers: true, GCArrayHelpers: true, SyncHostSlots: 6, GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantSafepoints := [][]uint32{
		{shared.RV64CallFrameHeaderBytes + 32, shared.RV64CallFrameHeaderBytes + 48, shared.RV64CallFrameHeaderBytes + 64, shared.RV64CallFrameHeaderBytes + 80},
		{shared.RV64CallFrameHeaderBytes + 32, shared.RV64CallFrameHeaderBytes + 48, shared.RV64CallFrameHeaderBytes + 64, shared.RV64CallFrameHeaderBytes + 80},
		{shared.RV64CallFrameHeaderBytes + 32, shared.RV64CallFrameHeaderBytes + 48, shared.RV64CallFrameHeaderBytes + 64, shared.RV64CallFrameHeaderBytes + 80},
		{shared.RV64CallFrameHeaderBytes + 32, shared.RV64CallFrameHeaderBytes + 48, shared.RV64CallFrameHeaderBytes + 64, shared.RV64CallFrameHeaderBytes + 80},
		{shared.RV64CallFrameHeaderBytes + 32, shared.RV64CallFrameHeaderBytes + 48, shared.RV64CallFrameHeaderBytes + 64, shared.RV64CallFrameHeaderBytes + 80},
	}
	var safepoints [][]uint32
	if !plan.VisitSafepoints(func(_ int, offsets []uint32) bool {
		safepoints = append(safepoints, append([]uint32(nil), offsets...))
		return true
	}) {
		t.Fatal("visit SIMD/array safepoints")
	}
	if len(compiled.Code) == 0 || plan.SafepointCount() != 5 || plan.CallsiteCount() != 0 || fmt.Sprint(safepoints) != fmt.Sprint(wantSafepoints) {
		t.Fatalf("SIMD/array code=%d safepoints=%v want=%v callsites=%d", len(compiled.Code), safepoints, wantSafepoints, plan.CallsiteCount())
	}
}

func rv64RootedScalarSegmentArrayModule() []byte {
	body := []byte{
		0x02, 0x01, 0x63, 0x00, 0x01, 0x63, 0x01,
		0x41, 0x00, 0x41, 0x03, 0xfb, 0x09, 0x00, 0x00, 0x22, 0x00,
		0x1a,
		0x20, 0x00, 0x41, 0x01, 0x41, 0x0c, 0x41, 0x01, 0xfb, 0x12, 0x00, 0x00,
		0x41, 0x00, 0x41, 0x02, 0xfb, 0x0a, 0x01, 0x00, 0x22, 0x01,
		0x1a,
		0x20, 0x01, 0x41, 0x00, 0xfb, 0x0b, 0x01, 0xfb, 0x1d,
		0x20, 0x01, 0x41, 0x00, 0x41, 0x01, 0x41, 0x01, 0xfb, 0x13, 0x01, 0x00,
		0x20, 0x00, 0x41, 0x00, 0xfb, 0x0b, 0x00,
		0x20, 0x00, 0x41, 0x01, 0xfb, 0x0b, 0x00,
		0x20, 0x01, 0x41, 0x00, 0xfb, 0x0b, 0x01, 0xfb, 0x1d,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5e, 0x7f, 0x01},
			[]byte{0x5e, byte(wasm.HeapI31), 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(9, []byte{0x01, 0x05, byte(wasm.HeapI31), 0x02,
			0x41, 0x7e, 0xfb, 0x1c, 0x0b,
			0x41, 0x7d, 0xfb, 0x1c, 0x0b,
		}),
		wasmtest.Section(12, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
		wasmtest.Section(11, wasmtest.Vec([]byte{0x01, 0x10,
			0x01, 0x00, 0x00, 0x00,
			0x02, 0x00, 0x00, 0x00,
			0x03, 0x00, 0x00, 0x00,
			0x04, 0x00, 0x00, 0x00,
		})),
	)
}

func TestRV64RootedScalarSegmentArraysCompile(t *testing.T) {
	source := rv64RootedScalarSegmentArrayModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if vector, err := functionUsesSWAR(m, 0); err != nil || vector {
		t.Fatalf("scalar segment-array selector=%t err=%v", vector, err)
	}
	roots := shared.NewGCModuleFrameRootPlan(1)
	if !roots.MarkFunction(0) || !roots.ReserveFunctions(1) {
		t.Fatal("reserve scalar segment-array roots")
	}
	plan, ok := roots.BeginFunction(0)
	if !ok {
		t.Fatal("begin scalar segment-array roots")
	}
	*plan = shared.GCFrameRootPlan{
		Candidate: true, Exact: true, Conservative: true,
		Locals: []shared.GCFrameLocal{
			{Index: 0, Offset: shared.RV64CallFrameHeaderBytes},
			{Index: 1, Offset: shared.RV64CallFrameHeaderBytes + 8},
		},
	}
	if !plan.SetLiveMasks([]uint64{3, 3}, 2, 0) {
		t.Fatal("install scalar segment-array allocation masks")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := CompileModuleWith(m, CompileOptions{
		DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true,
		GCArrayHelpers: true, SyncHostSlots: 6, GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Code) == 0 || plan.SafepointCount() != 2 || plan.CallsiteCount() != 0 {
		t.Fatalf("scalar segment-array code=%d safepoints=%d callsites=%d", len(compiled.Code), plan.SafepointCount(), plan.CallsiteCount())
	}
}

func rv64RootedReferenceHostImportModule() []byte {
	functionImport := append(append(wasmtest.Name("env"), wasmtest.Name("echo")...), byte(wasm.ExternFunc))
	functionImport = append(functionImport, wasmtest.ULEB(1)...)
	body := []byte{0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0x10, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			[]byte{0x60, 0x01, 0x64, 0x00, 0x01, 0x64, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(functionImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rv64RootedReferenceFunctionReexportModule() []byte {
	functionImport := append(append(wasmtest.Name("env"), wasmtest.Name("echo")...), byte(wasm.ExternFunc))
	functionImport = append(functionImport, wasmtest.ULEB(1)...)
	body := []byte{0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0x10, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			[]byte{0x60, 0x01, 0x64, 0x00, 0x01, 0x64, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(functionImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("echo", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedReferenceHostImportAdmitted(t *testing.T) {
	if err := AdmitProductionSource(rv64RootedReferenceHostImportModule()); err != nil {
		t.Fatalf("collector-reference host import was not admitted: %v", err)
	}
	if source := rv64RootedReferenceFunctionReexportModule(); !AdmitGCRootedScalarSource(source) {
		t.Fatal("collector-reference function re-export was not admitted")
	} else if err := AdmitProductionSource(source); err != nil {
		t.Fatalf("collector-reference function re-export failed production admission: %v", err)
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
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("root", byte(wasm.ExternGlobal), 0),
		)),
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
	state, err := admitStatePolicy(m, true, false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !state.importReferences || !state.localMutableReferences || len(state.globals) != 1 || !state.globals[0].wide || !state.globals[0].reference || !state.globals[0].mutable {
		t.Fatalf("imported rooted global state=%+v globals=%+v", state, state.globals)
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
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("abstract anyref global did not admit its i31 initializer")
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
	if !AdmitGCRootedScalarSource(rv64RootedI31TableModule(true)) {
		t.Fatal("public rooted i31 table was not admitted")
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

func rv64RootedAbstractTableModule() []byte {
	body := []byte{
		0x00,
		0x41, 0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0x26, 0x00,
		0x41, 0x01, 0x41, 0x7d, 0xfb, 0x1c, 0x26, 0x00,
		0x41, 0x09, 0xfb, 0x00, 0x00, 0x1a,
		0x41, 0x00, 0x25, 0x00, 0xfb, 0x16, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x41, 0x01, 0x25, 0x00, 0xfb, 0x16, byte(wasm.HeapI31), 0xfb, 0x1d,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, []byte{0x01, byte(wasm.HeapAny), 0x01, 0x02, 0x02}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedAbstractTableCompiles(t *testing.T) {
	source := rv64RootedAbstractTableModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("mixed abstract collector-reference table was not admitted")
	}
	for name, ref := range map[string]wasm.RefType{
		"non-null": wasm.Ref(false, wasm.AbsHeap(wasm.HeapAny), false),
		"exact":    wasm.Ref(true, wasm.AbsHeap(wasm.HeapAny), true),
		"none":     wasm.Ref(true, wasm.AbsHeap(wasm.HeapNone), false),
	} {
		candidate, decodeErr := wasm.DecodeModule(source)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		candidate.Tables[0].Type.Ref = ref
		if admitRV64GCRootedScalarModule(candidate) {
			t.Fatalf("%s abstract table was admitted", name)
		}
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
		GCStructHelpers: true, SyncHostSlots: 8, GCFrameRoots: roots, GCFrameRootsSource: true,
		Codegen:    codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}},
		production: true, publicStarts: true, gcRootedScalar: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.code) == 0 || len(compiled.functions) != 1 || plan.SafepointCount() != 2 {
		t.Fatalf("abstract table code/functions/safepoints = %d/%d/%d", len(compiled.code), len(compiled.functions), plan.SafepointCount())
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
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("root", byte(wasm.ExternGlobal), 0),
		)),
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

func rv64RootedImportedAggregateTableModule() []byte {
	tableImport := append(append(wasmtest.Name("provider"), wasmtest.Name("objects")...), byte(wasm.ExternTable))
	tableImport = append(tableImport, 0x63, 0x00, 0x01, 0x01, 0x02)
	body := []byte{0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0x1a, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("objects", byte(wasm.ExternTable), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedImportedAggregateTableAdmitted(t *testing.T) {
	source := rv64RootedImportedAggregateTableModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	if !AdmitGCRootedScalarSource(source) {
		t.Fatal("public rooted aggregate-table import was not admitted")
	}
}

func rv64RootedImportedAggregateGlobalModule() []byte {
	globalImport := append(append(wasmtest.Name("provider"), wasmtest.Name("object")...), byte(wasm.ExternGlobal))
	globalImport = append(globalImport, 0x63, 0x00, 0x01) // (mut (ref null 0))
	body := []byte{
		0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00, 0x1a,
		0x23, 0x00, 0xd4, 0xfb, 0x02, 0x00, 0x00,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(globalImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("object", byte(wasm.ExternGlobal), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedImportedAggregateGlobalAdmitted(t *testing.T) {
	source := rv64RootedImportedAggregateGlobalModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	if !AdmitGCRootedScalarSource(source) {
		t.Fatal("public rooted aggregate-global import was not admitted")
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
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
	m.Imports[0].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{}), true)), Mutable: true})
	if admitRV64GCRootedScalarModule(m) {
		t.Fatal("exact rooted aggregate-global import was admitted")
	}
}

func rv64RootedImportedAbstractGlobalModule(mutable bool) []byte {
	mut := byte(0)
	if mutable {
		mut = 1
	}
	globalImport := append(append(wasmtest.Name("provider"), wasmtest.Name("root")...), byte(wasm.ExternGlobal), byte(wasm.HeapAny), mut)
	readObject := []byte{0x00, 0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a, 0x23, 0x00, 0xfb, 0x16, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	readI31 := []byte{0x00, 0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a, 0x23, 0x00, 0xfb, 0x16, byte(wasm.HeapI31), 0xfb, 0x1d, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(globalImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("read_object", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("read_i31", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("root", byte(wasm.ExternGlobal), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(readObject))), readObject...),
			append(wasmtest.ULEB(uint32(len(readI31))), readI31...),
		)),
	)
}

func TestRV64RootedImportedAbstractGlobalsAdmitted(t *testing.T) {
	for _, mutable := range []bool{false, true} {
		source := rv64RootedImportedAbstractGlobalModule(mutable)
		if err := AdmitProductionSource(source); err != nil {
			t.Fatalf("mutable=%t: %v", mutable, err)
		}
		if !AdmitGCRootedScalarSource(source) {
			t.Fatalf("mutable=%t: abstract collector-reference global import was not admitted", mutable)
		}
		for _, heap := range []wasm.AbsHeapType{wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray} {
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			m.Imports[0].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(heap), false)), Mutable: mutable})
			if !admitRV64GCRootedScalarModule(m) {
				t.Fatalf("mutable=%t heap=%v: abstract collector-reference global import was not admitted", mutable, heap)
			}
		}
	}
	m, err := wasm.DecodeModule(rv64RootedImportedAbstractGlobalModule(false))
	if err != nil {
		t.Fatal(err)
	}
	m.Imports[0].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapAny), false))})
	if admitRV64GCRootedScalarModule(m) {
		t.Fatal("non-null abstract collector-reference global import was admitted")
	}
}

func rv64RootedImportedAggregateGlobalCopyModule() []byte {
	globalImport := append(append(wasmtest.Name("provider"), wasmtest.Name("object")...), byte(wasm.ExternGlobal))
	globalImport = append(globalImport, 0x63, 0x00, 0x00) // immutable (ref null 0)
	body := []byte{
		0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00, 0x1a,
		0x23, 0x01, 0xd4, 0xfb, 0x02, 0x00, 0x00,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(globalImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x00, 0x00, 0x23, 0x00, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("copy", byte(wasm.ExternGlobal), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedImportedAggregateGlobalInitializerCompiles(t *testing.T) {
	source := rv64RootedImportedAggregateGlobalCopyModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	if !AdmitGCRootedScalarSource(source) {
		t.Fatal("imported collector-reference global initializer was not admitted")
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
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
	m.Imports[0].Type = wasm.NewGlobalExternType(wasm.GlobalType{Type: wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{}), false)), Mutable: true})
	if admitRV64GCRootedScalarModule(m) {
		t.Fatal("mutable imported collector-reference initializer source was admitted")
	}
}

func rv64RootedImportedI31GlobalCopyModule() []byte {
	globalImport := append(append(wasmtest.Name("provider"), wasmtest.Name("immediate")...), byte(wasm.ExternGlobal), wasm.MustEncodeValType(wasm.I31Ref), 0x00)
	body := []byte{
		0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00, 0x1a,
		0x23, 0x01, 0xfb, 0x1d,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(globalImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I31Ref, false, []byte{0x23, 0x00, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("copy", byte(wasm.ExternGlobal), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedImportedI31GlobalInitializerCompiles(t *testing.T) {
	source := rv64RootedImportedI31GlobalCopyModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	if !AdmitGCRootedScalarSource(source) {
		t.Fatal("imported i31 global initializer was not admitted")
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
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

func rv64RootedAbstractStructGlobalModule() []byte {
	body := []byte{
		0x00,
		0x41, 0x7e, 0xfb, 0x1c, 0x24, 0x00,
		0x23, 0x00, 0xfb, 0x14, byte(wasm.HeapI31), 0x1a,
		0x41, 0x29, 0xfb, 0x00, 0x00, 0x24, 0x00,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		0x23, 0x00, 0xfb, 0x16, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x41, 0x01, 0x6a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, []byte{0x01, byte(wasm.HeapAny), 0x01, 0x41, 0x28, 0xfb, 0x00, 0x00, 0x0b}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedAbstractStructGlobalCompiles(t *testing.T) {
	source := rv64RootedAbstractStructGlobalModule()
	if err := AdmitProductionSource(source); err != nil {
		t.Fatal(err)
	}
	if !AdmitGCRootedScalarSource(source) {
		t.Fatal("abstract anyref global was not admitted")
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	state, err := admitStatePolicy(m, true, false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.globals) != 1 || !state.globals[0].wide || !state.globals[0].reference || !state.globals[0].mutable {
		t.Fatalf("abstract rooted global state=%+v globals=%+v", state, state.globals)
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
	m.Globals[0].Type.Type = wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapAny), false))
	if admitRV64GCRootedScalarModule(m) {
		t.Fatal("non-null abstract collector-reference global was admitted")
	}
	m.Globals[0].Type.Type = wasm.RefVal(wasm.Ref(true, wasm.AbsHeap(wasm.HeapAny), false))
	m.Exports = append(m.Exports, wasm.Export{Name: "root", Index: wasm.ExternIdx{Kind: wasm.ExternGlobal, Index: 0}})
	if !admitRV64GCRootedScalarModule(m) {
		t.Fatal("abstract collector-reference global was not admitted through the public boundary")
	}
}

func rv64RootedDirectReferenceGlobalTableInitializerModule(imported bool) []byte {
	body := []byte{
		0x00,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		0x41, 0x00, 0x25, 0x00, 0xd4, 0xfb, 0x02, 0x00, 0x00,
		0x0b,
	}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
	}
	if imported {
		globalImport := append(append(wasmtest.Name("provider"), wasmtest.Name("g")...), byte(wasm.ExternGlobal), 0x63, 0x00, 0x00)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(globalImport)))
	}
	sections = append(sections,
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, []byte{0x01, 0x40, 0x00, 0x63, 0x00, 0x01, 0x02, 0x02, 0x23, 0x00, 0x0b}),
	)
	if !imported {
		sections = append(sections, wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0x00, 0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0x0b})))
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("read", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	return wasmtest.Module(sections...)
}

func TestRV64RootedDirectReferenceGlobalTableInitializersCompile(t *testing.T) {
	for _, imported := range []bool{false, true} {
		t.Run(map[bool]string{false: "local", true: "imported"}[imported], func(t *testing.T) {
			source := rv64RootedDirectReferenceGlobalTableInitializerModule(imported)
			if err := AdmitProductionSource(source); err != nil {
				t.Fatal(err)
			}
			if !AdmitGCRootedScalarSource(source) {
				t.Fatal("direct collector-reference global table initializer was not admitted")
			}
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
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
