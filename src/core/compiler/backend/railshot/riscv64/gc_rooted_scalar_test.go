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
