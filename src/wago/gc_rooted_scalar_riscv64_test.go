//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo && !wago_precompiled

package wago

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64RootedScalarConfig() *RuntimeConfig {
	return rv64I31Config().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTypedFunctionReferences)
}

func rootedInternalReferenceCallModule(exportInternal bool) []byte {
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

func TestRV64RootedInternalReferenceCallExecutes(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedInternalReferenceCallModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	roots := view.genericGCFrameRoots()
	if !rv64FreshRootedGC(view) || roots == nil || len(roots.safepoints) != 3 || len(roots.callsites) != 1 || len(roots.callsites[0].offsets) != 1 {
		t.Fatalf("rooted call receipt=%t roots=%+v", rv64FreshRootedGC(view), roots)
	}
	mutated := *view
	mutated.Exports = map[string]int{"run": 0}
	if rv64FreshRootedGC(&mutated) {
		t.Fatal("collector reference export retained the rooted compiler receipt")
	}
	mutated = *view
	mutated.Funcs = slices.Clone(view.Funcs)
	mutated.Funcs[0].TypeIndex = 0
	if rv64FreshRootedGC(&mutated) {
		t.Fatal("mutated internal function binding retained the rooted compiler receipt")
	}
	mutated = *view
	mutated.FuncTypeID = slices.Clone(view.FuncTypeID)
	mutated.FuncTypeID[0] ^= 1
	if rv64FreshRootedGC(&mutated) {
		t.Fatal("mutated internal function type key retained the rooted compiler receipt")
	}
	mutated = *view
	mutated.Types = slices.Clone(view.Types)
	mutated.Types[1].Params = make([]ValueTypeDescriptor, 257)
	if rv64FreshRootedGC(&mutated) {
		t.Fatal("oversized internal function signature retained the rooted compiler receipt")
	}
	rt := NewRuntime(WithRuntimeConfig(rv64RootedScalarConfig()))
	defer rt.Close()
	module, err := rt.Module(compiled)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := rt.Instantiate(context.Background(), module, WithImports(NewImports()), WithGC(GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}))
	module.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for iteration := range 32 {
		got, err := instance.Invoke("run")
		if err != nil || !slices.Equal(got, []uint64{83}) {
			t.Fatalf("iteration %d: run()=%v err=%v want=[83]", iteration, got, err)
		}
	}
}

func TestRV64RootedReferenceExportFailsClosed(t *testing.T) {
	if _, err := Compile(rv64RootedScalarConfig(), rootedInternalReferenceCallModule(true)); err == nil {
		t.Fatal("collector reference export unexpectedly compiled")
	}
}

func rootedScalarFunctionBeforeStructModule() []byte {
	body := []byte{0x00, 0x41, 0x2a, 0xfb, 0x00, 0x01, 0xfb, 0x02, 0x01, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			[]byte{0x5f, 0x01, 0x7f, 0x00},
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedScalarFunctionBeforeStructExecutes(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedScalarFunctionBeforeStructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if len(instance.serArgs) < 5*8 {
		t.Fatalf("generic struct helper argument arena = %d bytes, want at least 40", len(instance.serArgs))
	}
	got, err := instance.Invoke("run")
	if err != nil || !slices.Equal(got, []uint64{42}) {
		t.Fatalf("run()=%v err=%v want=[42]", got, err)
	}
}

func rootedScalarMutableStructModule() []byte {
	body := []byte{0x01, 0x01, 0x64, 0x01, 0x41, 0x0b, 0xfb, 0x00, 0x01, 0x21, 0x00, 0x20, 0x00, 0x41, 0x2a, 0xfb, 0x05, 0x01, 0x00, 0x20, 0x00, 0xfb, 0x02, 0x01, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			[]byte{0x5f, 0x01, 0x7f, 0x01},
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedScalarMutableStructExecutes(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedScalarMutableStructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run")
	if err != nil || !slices.Equal(got, []uint64{42}) {
		t.Fatalf("run()=%v err=%v want=[42]", got, err)
	}
}

func rootedScalarMultiFieldStructModule() []byte {
	body := []byte{
		0x01, 0x01, 0x64, 0x01,
		0x41, 0x01, 0x41, 0x02, 0x41, 0x03, 0x41, 0x04, 0x41, 0x05, 0xfb, 0x00, 0x01, 0x21, 0x00,
		0x20, 0x00, 0x41, 0x28, 0xfb, 0x05, 0x01, 0x03,
		0x20, 0x00, 0xfb, 0x02, 0x01, 0x00,
		0x20, 0x00, 0xfb, 0x02, 0x01, 0x03, 0x6a,
		0x20, 0x00, 0xfb, 0x02, 0x01, 0x04, 0x6a, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			[]byte{0x5f, 0x05, 0x7f, 0x00, 0x7f, 0x00, 0x7f, 0x00, 0x7f, 0x01, 0x7f, 0x00},
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedScalarMultiFieldStructExecutes(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedScalarMultiFieldStructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run")
	if err != nil || !slices.Equal(got, []uint64{46}) {
		t.Fatalf("run()=%v err=%v want=[46]", got, err)
	}
}

func rootedMixedScalarStructModule(result wasm.ValType, field byte) []byte {
	types := [][]byte{
		wasmtest.FuncType(nil, []wasm.ValType{result}),
		{0x5f, 0x04, 0x7f, 0x01, 0x7e, 0x01, 0x7d, 0x01, 0x7c, 0x01},
	}
	body := []byte{
		0x01, 0x01, 0x64, 0x01,
		0x41, 0x01,
		0x42, 0x02,
		0x43, 0x00, 0x00, 0x50, 0x40,
		0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x12, 0x40,
		0xfb, 0x00, 0x01, 0x21, 0x00, 0x20, 0x00,
	}
	values := [][]byte{
		{0x41, 0x09},
		{0x42, 0x2a},
		{0x43, 0x00, 0x00, 0xc8, 0x40},
		{0x44, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x1e, 0x40},
	}
	body = append(body, values[field]...)
	body = append(body, 0xfb, 0x05, 0x01, field, 0x20, 0x00, 0xfb, 0x02, 0x01, field, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedMixedScalarStructExecutes(t *testing.T) {
	for field, tc := range []struct {
		typ  wasm.ValType
		want uint64
	}{{wasm.I32, 9}, {wasm.I64, 42}, {wasm.F32, 0x40c80000}, {wasm.F64, 0x401e000000000000}} {
		t.Run(fmt.Sprintf("field=%d", field), func(t *testing.T) {
			compiled, err := Compile(rv64RootedScalarConfig(), rootedMixedScalarStructModule(tc.typ, byte(field)))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			if !rv64FreshRootedGC(compiled.executionView()) {
				t.Fatal("fresh rooted array compiler receipt is missing")
			}
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			got, err := instance.Invoke("run")
			if err != nil || !slices.Equal(got, []uint64{tc.want}) {
				t.Fatalf("run()=%#x err=%v want=[%#x]", got, err, tc.want)
			}
		})
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

func TestRV64RootedPackedScalarStructExecutes(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedPackedScalarStructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	want := []uint64{0xffffffff, 0xff, 0xffffffff, 0xffff}
	if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, want) {
		t.Fatalf("run()=%#x err=%v want=%#x", got, err, want)
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

func rootedPackedScalarArrayModule() []byte {
	types := [][]byte{
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32}),
		{0x5e, byte(wasm.PackI8), 0x01},
	}
	body := []byte{
		0x01, 0x01, 0x64, 0x01,
		0x41, 0x02, 0xfb, 0x07, 0x01, 0x21, 0x00,
		0x20, 0x00, 0x41, 0x01, 0x41, 0xff, 0x01, 0xfb, 0x0e, 0x01,
		0x20, 0x00, 0x41, 0x01, 0xfb, 0x0c, 0x01,
		0x20, 0x00, 0x41, 0x01, 0xfb, 0x0d, 0x01,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rootedWideScalarArrayBulkModule() []byte {
	const value = int64(0x0102030405060708)
	types := [][]byte{
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I64, wasm.I64}),
		{0x5e, 0x7e, 0x01},
	}
	body := []byte{
		0x01, 0x02, 0x64, 0x01,
		0x41, 0x04, 0xfb, 0x07, 0x01, 0x21, 0x00,
		0x41, 0x04, 0xfb, 0x07, 0x01, 0x21, 0x01,
		0x20, 0x00, 0x41, 0x01, 0x42,
	}
	body = append(body, wasmtest.SLEB64(value)...)
	body = append(body,
		0x41, 0x02, 0xfb, 0x10, 0x01,
		0x20, 0x01, 0x41, 0x00, 0x20, 0x00, 0x41, 0x01, 0x41, 0x02, 0xfb, 0x11, 0x01, 0x01,
		0x20, 0x01, 0x41, 0x00, 0xfb, 0x0b, 0x01,
		0x20, 0x01, 0x41, 0x01, 0xfb, 0x0b, 0x01,
		0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rootedPackedScalarArrayBulkModule() []byte {
	types := [][]byte{
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32}),
		{0x5e, byte(wasm.PackI8), 0x01},
	}
	body := []byte{
		0x01, 0x02, 0x64, 0x01,
		0x41, 0x03, 0xfb, 0x07, 0x01, 0x21, 0x00,
		0x41, 0x03, 0xfb, 0x07, 0x01, 0x21, 0x01,
		0x20, 0x00, 0x41, 0x01, 0x41, 0xff, 0x01, 0x41, 0x01, 0xfb, 0x10, 0x01,
		0x20, 0x01, 0x41, 0x00, 0x20, 0x00, 0x41, 0x01, 0x41, 0x01, 0xfb, 0x11, 0x01, 0x01,
		0x20, 0x01, 0x41, 0x00, 0xfb, 0x0c, 0x01,
		0x20, 0x01, 0x41, 0x00, 0xfb, 0x0d, 0x01,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rootedWideScalarArrayFixedModule() []byte {
	const last = int64(0x0102030405060708)
	types := [][]byte{
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
		{0x5e, 0x7e, 0x01},
	}
	body := []byte{0x00}
	for value := int64(1); value <= 5; value++ {
		body = append(body, 0x42)
		body = append(body, wasmtest.SLEB64(value)...)
	}
	body = append(body, 0x42)
	body = append(body, wasmtest.SLEB64(last)...)
	body = append(body,
		0xfb, 0x08, 0x01, 0x06,
		0x41, 0x05, 0xfb, 0x0b, 0x01,
		0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rootedWideScalarArrayFixedEdgeModule(count byte) []byte {
	const value = int64(0x0102030405060708)
	result := wasm.I32
	body := []byte{0x00}
	if count == 1 {
		result = wasm.I64
		body = append(body, 0x42)
		body = append(body, wasmtest.SLEB64(value)...)
	}
	body = append(body, 0xfb, 0x08, 0x01, count)
	if count == 0 {
		body = append(body, 0xfb, 0x0f)
	} else {
		body = append(body, 0x41, 0x00, 0xfb, 0x0b, 0x01)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{result}),
			[]byte{0x5e, 0x7e, 0x01},
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rootedWideScalarArrayFixedBoundModule(count uint32) []byte {
	body := []byte{0x00}
	for value := uint32(0); value < count; value++ {
		body = append(body, 0x42)
		body = append(body, wasmtest.SLEB64(int64(value))...)
	}
	body = append(body, 0xfb, 0x08, 0x01)
	body = append(body, wasmtest.ULEB(count)...)
	body = append(body, 0xfb, 0x0f, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			[]byte{0x5e, 0x7e, 0x01},
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rootedScalarArrayNullModule() []byte {
	types := [][]byte{
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		{0x5e, 0x7f, 0x01},
	}
	body := []byte{
		0x00,
		0x41, 0x01, 0xfb, 0x07, 0x01, 0x1a,
		0xd0, 0x01, 0xd1,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedScalarArraysExecute(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source []byte
		want   []uint64
	}{
		{name: "wide", source: rootedWideScalarArrayModule(), want: []uint64{42, 2}},
		{name: "packed", source: rootedPackedScalarArrayModule(), want: []uint64{0xffffffff, 0xff}},
		{name: "wide-bulk", source: rootedWideScalarArrayBulkModule(), want: []uint64{0x0102030405060708, 0x0102030405060708}},
		{name: "packed-bulk", source: rootedPackedScalarArrayBulkModule(), want: []uint64{0xffffffff, 0xff}},
		{name: "wide-fixed", source: rootedWideScalarArrayFixedModule(), want: []uint64{0x0102030405060708}},
		{name: "wide-fixed-empty", source: rootedWideScalarArrayFixedEdgeModule(0), want: []uint64{0}},
		{name: "wide-fixed-one", source: rootedWideScalarArrayFixedEdgeModule(1), want: []uint64{0x0102030405060708}},
		{name: "wide-fixed-bound", source: rootedWideScalarArrayFixedBoundModule(254), want: []uint64{254}},
		{name: "indexed-null", source: rootedScalarArrayNullModule(), want: []uint64{1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(rv64RootedScalarConfig(), tc.source)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, tc.want) {
				t.Fatalf("run()=%#x err=%v want=%#x", got, err, tc.want)
			}
		})
	}
}

func TestRV64RootedScalarArrayFixedRejectsBeyondHelperFrame(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedWideScalarArrayFixedBoundModule(255))
	if compiled != nil {
		compiled.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "generic rooted scalar GC helper contract") {
		t.Fatalf("255-element array.new_fixed error = %v", err)
	}
}

func rootedScalarArrayStressModule() []byte {
	types := [][]byte{
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}),
		{0x5e, 0x7e, 0x01},
	}
	body := []byte{
		0x01, 0x01, 0x64, 0x01,
		0x42, 0x2a, 0x41, 0x01, 0xfb, 0x06, 0x01, 0x21, 0x00,
		0x41, 0x01, 0xfb, 0x07, 0x01, 0x1a,
		0x20, 0x00, 0x41, 0x00, 0xfb, 0x0b, 0x01,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedScalarArrayStressGCLifetime(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedScalarArrayStressModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	rt := NewRuntime(WithRuntimeConfig(rv64RootedScalarConfig()))
	defer rt.Close()
	module, err := rt.Module(compiled)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := rt.Instantiate(context.Background(), module, WithImports(NewImports()), WithGC(GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}))
	module.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if instance.gc == nil || !instance.gc.CollectsOnAllocation() {
		t.Fatal("generic rooted array stress collector is not active")
	}
	for iteration := range 16 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{42}) {
			t.Fatalf("iteration %d: run()=%v err=%v want=[42]", iteration, got, err)
		}
	}
}

func rootedReferenceFieldStructModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x01},       // mutable i32 leaf
		{0x5f, 0x01, 0x63, 0x00, 0x01}, // mutable (ref null 0) holder
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	body := []byte{
		0x02, 0x01, 0x64, 0x00, 0x01, 0x64, 0x01,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x20, 0x00, 0xfb, 0x00, 0x01, 0x21, 0x01,
		0x41, 0x2a, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x20, 0x01, 0x20, 0x00, 0xfb, 0x05, 0x01, 0x00,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x01, 0xfb, 0x02, 0x01, 0x00,
		0xfb, 0x02, 0x00, 0x00,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedReferenceFieldStructExecutes(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedReferenceFieldStructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{42}) {
		t.Fatalf("run()=%v err=%v want=[42]", got, err)
	}
}

func TestRV64RootedReferenceFieldStructStressGC(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedReferenceFieldStructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	rt := NewRuntime(WithRuntimeConfig(rv64RootedScalarConfig()))
	defer rt.Close()
	module, err := rt.Module(compiled)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := rt.Instantiate(context.Background(), module, WithImports(NewImports()), WithGC(GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}))
	module.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for iteration := range 16 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{42}) {
			t.Fatalf("iteration %d: run()=%v err=%v want=[42]", iteration, got, err)
		}
	}
}

func rootedReferenceArrayModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x01}, // mutable i32 leaf
		{0x5e, 0x63, 0x00, 0x01}, // mutable array of (ref null 0)
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32}),
	}
	body := []byte{
		0x02, 0x01, 0x64, 0x00, 0x03, 0x64, 0x01,
		// The uniform initializer exists only on the operand stack while the
		// array allocation may collect.
		0x41, 0x07, 0xfb, 0x00, 0x00,
		0x41, 0x02, 0xfb, 0x06, 0x01, 0x21, 0x01,
		0x41, 0x02, 0xfb, 0x07, 0x01, 0x21, 0x02,
		0x41, 0x2a, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x20, 0x02, 0x41, 0x00, 0x20, 0x00, 0x41, 0x02, 0xfb, 0x10, 0x01,
		0x20, 0x01, 0x41, 0x00, 0x20, 0x00, 0xfb, 0x0e, 0x01,
		0x20, 0x01, 0x41, 0x01, 0x20, 0x02, 0x41, 0x00, 0x41, 0x01, 0xfb, 0x11, 0x01, 0x01,
		// Both fixed initializers remain stack roots while the second leaf and
		// then the array itself allocate.
		0x41, 0x09, 0xfb, 0x00, 0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00,
		0xfb, 0x08, 0x01, 0x02, 0x21, 0x03,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x01, 0x41, 0x01, 0xfb, 0x0b, 0x01, 0xfb, 0x02, 0x00, 0x00,
		0x20, 0x03, 0x41, 0x01, 0xfb, 0x0b, 0x01, 0xfb, 0x02, 0x00, 0x00,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedReferenceArrayExecutes(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedReferenceArrayModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{42, 42}) {
		t.Fatalf("run()=%v err=%v want=[42 42]", got, err)
	}
}

func TestRV64RootedReferenceArrayStressGC(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedReferenceArrayModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	rt := NewRuntime(WithRuntimeConfig(rv64RootedScalarConfig()))
	defer rt.Close()
	module, err := rt.Module(compiled)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := rt.Instantiate(context.Background(), module, WithImports(NewImports()), WithGC(GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}))
	module.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for iteration := range 16 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{42, 42}) {
			t.Fatalf("iteration %d: run()=%v err=%v want=[42 42]", iteration, got, err)
		}
	}
}

func rootedIndexedReferenceTestCastModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x01}, // mutable i32 leaf
		{0x5e, 0x63, 0x00, 0x01}, // mutable array of (ref null 0)
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}),
	}
	body := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x20, 0x00, 0xfb, 0x14, 0x00,
		0x20, 0x00, 0xfb, 0x14, 0x01,
		0x20, 0x00, 0xfb, 0x16, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0xd0, 0x00, 0xfb, 0x15, 0x00,
		0xd0, 0x00, 0xfb, 0x17, 0x00, 0xd1,
		0x20, 0x00, 0xfb, 0x14, byte(wasm.HeapStruct),
		0x20, 0x00, 0xfb, 0x14, byte(wasm.HeapArray),
		0x20, 0x00, 0xfb, 0x14, byte(wasm.HeapEq),
		0x20, 0x00, 0xfb, 0x16, byte(wasm.HeapStruct), 0xfb, 0x16, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x41, 0x00, 0xfb, 0x07, 0x01, 0xfb, 0x14, byte(wasm.HeapArray),
		0xd0, 0x00, 0xfb, 0x15, byte(wasm.HeapNone),
		0xd0, 0x00, 0xfb, 0x17, byte(wasm.HeapNone), 0xd1,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedIndexedReferenceTestCastExecutes(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedIndexedReferenceTestCastModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	want := []uint64{1, 0, 42, 1, 1, 1, 0, 1, 42, 1, 1, 1}
	if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, want) {
		t.Fatalf("run()=%v err=%v want=%v", got, err, want)
	}
}

func TestRV64RootedUnsupportedAbstractCastFailsClosed(t *testing.T) {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x01},
		wasmtest.FuncType(nil, nil),
	}
	body := []byte{
		0x00,
		0x41, 0x01, 0xfb, 0x00, 0x00,
		0xfb, 0x16, byte(wasm.HeapEq),
		0xfb, 0x16, byte(wasm.HeapI31),
		0x1a, 0x0b,
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if compiled != nil {
		compiled.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "ref.cast heap outside generic rooted profile") {
		t.Fatalf("unsupported abstract ref.cast error=%v", err)
	}
}

func rootedArrayOnlyReferenceTestModule() []byte {
	types := [][]byte{
		{0x5e, 0x7f, 0x01},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	body := []byte{0x00, 0x41, 0x00, 0xfb, 0x07, 0x00, 0xfb, 0x14, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedArrayOnlyReferenceTestExecutes(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedArrayOnlyReferenceTestModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{1}) {
		t.Fatalf("run()=%v err=%v want=[1]", got, err)
	}
}

func rootedIndexedReferenceCastFailureModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x01}, // mutable i32 leaf
		{0x5e, 0x63, 0x00, 0x01}, // mutable array of (ref null 0)
		wasmtest.FuncType(nil, nil),
	}
	body := []byte{0x00, 0x41, 0x01, 0xfb, 0x00, 0x00, 0xfb, 0x16, 0x01, 0x1a, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedIndexedReferenceCastTraps(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedIndexedReferenceCastFailureModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("run"); err == nil {
		t.Fatal("mismatched rooted ref.cast did not trap")
	} else if trap, ok := err.(*TrapError); !ok || trap.Code != TrapCastFailure {
		t.Fatalf("mismatched rooted ref.cast trap=%v, want %v", err, TrapCastFailure)
	}
}

func rootedIndexedBranchCastModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},             // immutable struct (field i32)
		{0x60, 0x00, 0x01, 0x64, 0x00},       // () -> (ref 0)
		{0x60, 0x00, 0x01, 0x63, 0x00},       // () -> (ref null 0)
		{0x60, 0x00, 0x02, 0x7f, 0x64, 0x00}, // () -> (i32, (ref 0))
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}),
	}
	body := []byte{
		0x01, 0x01, 0x7f, // one i32 local

		// A successful indexed br_on_cast carries the original object to the label.
		0x02, 0x01,
		0x41, 0x29, 0xfb, 0x00, 0x00,
		0xfb, 0x18, 0x00, 0x00, byte(wasm.HeapAny), 0x00,
		0x1a, 0x41, 0xe3, 0x00, 0xfb, 0x00, 0x00,
		0x0b, 0xfb, 0x02, 0x00, 0x00,

		// Null fails a nullable-source to non-null-target cast and takes fallback.
		0x02, 0x01,
		0xd0, 0x00,
		0xfb, 0x18, 0x01, 0x00, byte(wasm.HeapAny), 0x00,
		0x1a, 0x41, 0xcd, 0x00, 0xfb, 0x00, 0x00,
		0x0b, 0xfb, 0x02, 0x00, 0x00,

		// A successful br_on_cast_fail falls through with the refined reference.
		0x02, 0x01,
		0x41, 0x2a, 0xfb, 0x00, 0x00,
		0xfb, 0x19, 0x00, 0x00, 0x00, 0x00,
		0xfb, 0x02, 0x00, 0x00, 0x21, 0x00,
		0x41, 0x2b, 0xfb, 0x00, 0x00,
		0x0b, 0x1a, 0x20, 0x00,

		// A failed br_on_cast_fail branches with the original nullable source.
		0x02, 0x02,
		0xd0, 0x00,
		0xfb, 0x19, 0x01, 0x00, 0x00, 0x00,
		0x1a, 0x41, 0xe3, 0x00, 0xfb, 0x00, 0x00,
		0x0b, 0xd1,

		// A nullable target accepts null and branches with it on br_on_cast.
		0x02, 0x02,
		0xd0, 0x00,
		0xfb, 0x18, 0x03, 0x00, 0x00, 0x00,
		0x1a, 0x41, 0x37, 0xfb, 0x00, 0x00,
		0x0b, 0xd1,

		// The same nullable-target success falls through on br_on_cast_fail.
		0x41, 0x00, 0x21, 0x00,
		0x02, 0x01,
		0xd0, 0x00,
		0xfb, 0x19, 0x03, 0x00, 0x00, 0x00,
		0xd1, 0x21, 0x00,
		0x41, 0x37, 0xfb, 0x00, 0x00,
		0x0b, 0x1a, 0x20, 0x00,

		// A successful branch transports the typed label's scalar prefix too.
		0x02, 0x03,
		0x41, 0x09,
		0x41, 0x2c, 0xfb, 0x00, 0x00,
		0xfb, 0x18, 0x00, 0x00, 0x00, 0x00,
		0x1a, 0x1a, 0x41, 0xe3, 0x00, 0x41, 0x37, 0xfb, 0x00, 0x00,
		0x0b, 0xfb, 0x02, 0x00, 0x00, 0x6a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(4))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedIndexedBranchCastsExecute(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedIndexedBranchCastModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	want := []uint64{41, 77, 42, 1, 1, 1, 53}
	for _, gc := range []GCConfig{{}, {CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}} {
		instance, err := Instantiate(compiled, InstantiateOptions{GC: gc})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, want) {
			_ = instance.Close()
			t.Fatalf("run()=%v err=%v want=%v", got, err, want)
		}
		if err = instance.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func rootedBranchCastHelperOnlyModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},                         // immutable struct (field i32)
		{0x5e, 0x7f, 0x00},                               // immutable i32 array
		{0x60, 0x00, 0x01, 0x64, 0x00},                   // () -> (ref 0), used as the block signature
		{0x60, 0x01, 0x63, 0x00, 0x01, 0x7f},             // ((ref null 0)) -> i32
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), // public scalar entry
	}
	cast := []byte{
		0x00,
		0x02, 0x02,
		0x20, 0x00,
		0xfb, 0x18, 0x01, 0x00, 0x00, 0x00,
		0x1a, 0x41, 0xcd, 0x00, 0x0f,
		0x0b, 0x1a, 0x41, 0x01, 0x0b,
	}
	// The unrelated empty array allocation supplies the generic profile's
	// required allocation safepoint while leaving branch-cast as the only
	// struct-helper operation in the module.
	caller := []byte{0x00, 0x41, 0x00, 0xfb, 0x07, 0x01, 0x1a, 0xd0, 0x00, 0x10, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(3), wasmtest.ULEB(4))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(cast))), cast...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func TestRV64RootedBranchCastOnlyHelperExecutes(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedBranchCastHelperOnlyModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{77}) {
		t.Fatalf("run()=%v err=%v want=[77]", got, err)
	}
}

func rootedAbstractReferenceCallModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},       // immutable struct (field i32)
		{0x60, 0x00, 0x01, 0x64, 0x6b}, // () -> (ref struct), used as the block signature
		{0x60, 0x01, 0x6e, 0x01, 0x6b}, // (anyref) -> structref
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	cast := []byte{
		0x00,
		0x02, 0x01,
		0x20, 0x00,
		0xfb, 0x18, 0x01, 0x00, byte(wasm.HeapAny), byte(wasm.HeapStruct),
		0x00,
		0x0b,
		0x0b,
	}
	caller := []byte{
		0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00,
		0x10, 0x00,
		0xfb, 0x16, 0x00,
		0xfb, 0x02, 0x00, 0x00,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(cast))), cast...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func TestRV64RootedAbstractReferenceCallExecutes(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedAbstractReferenceCallModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || view.Types[2].Params[0].Ref.Heap.Abstract != AbstractHeapAny || view.Types[2].Results[0].Ref.Heap.Abstract != AbstractHeapStruct {
		t.Fatal("generic abstract reference signature receipt is missing")
	}
	for name, mutate := range map[string]func(*Compiled){
		"unsupported-heap": func(candidate *Compiled) { candidate.Types[2].Params[0].Ref.Heap.Abstract = AbstractHeapFunc },
		"abstract-index":   func(candidate *Compiled) { candidate.Types[2].Params[0].Ref.Heap.TypeIndex = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			candidate.validateMemo = view.validateMemo
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("hostile abstract signature retained generic receipt")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatalf("hostile abstract signature validation=%v", err)
			}
		})
	}
	for _, gc := range []GCConfig{{}, {CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}} {
		instance, err := Instantiate(compiled, InstantiateOptions{GC: gc})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{42}) {
			_ = instance.Close()
			t.Fatalf("run()=%v err=%v want=[42]", got, err)
		}
		if err = instance.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRV64RootedAbstractReferenceAdmissionFailsClosed(t *testing.T) {
	source := rootedAbstractReferenceCallModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("generic abstract object signature was not source-admitted")
	}
	needle := []byte{0x60, 0x01, byte(wasm.HeapAny), 0x01, byte(wasm.HeapStruct)}
	at := bytes.Index(source, needle)
	if at < 0 {
		t.Fatal("abstract signature encoding not found")
	}
	for _, heap := range []wasm.AbsHeapType{wasm.HeapI31, wasm.HeapFunc, wasm.HeapExtern} {
		candidate := slices.Clone(source)
		candidate[at+2] = byte(heap)
		if railshot.AdmitGCRootedScalarSource(candidate) {
			t.Fatalf("unsupported abstract heap %#x was source-admitted", byte(heap))
		}
	}
}

func rootedScalarMemoryModule(grow, activeData, passiveData, memoryExport bool) []byte {
	results := []wasm.ValType{wasm.I32, wasm.I32}
	if grow {
		results = append(results, wasm.I32, wasm.I32)
	}
	if activeData || passiveData {
		results = append(results, wasm.I32)
	}
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, results),
	}
	body := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		0x41, 0x10, 0x41, 0xe4, 0x00, 0x36, 0x02, 0x00,
		0x41, 0x10, 0x28, 0x02, 0x00,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x6a,
		0x3f, 0x00,
	}
	if grow {
		body = append(body, 0x41, 0x01, 0x40, 0x00, 0x3f, 0x00)
	}
	if activeData || passiveData {
		body = append(body, 0x41, 0x20, 0x28, 0x02, 0x00)
	}
	body = append(body, 0x0b)
	exports := [][]byte{wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0)}
	if memoryExport {
		exports = append(exports, wasmtest.ExportEntry("memory", byte(wasm.ExternMem), 0))
	}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x02})),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	}
	if activeData {
		sections = append(sections, wasmtest.Section(11, wasmtest.Vec([]byte{0x00, 0x41, 0x20, 0x0b, 0x04, 0x2a, 0x00, 0x00, 0x00})))
	} else if passiveData {
		sections = append(sections, wasmtest.Section(11, wasmtest.Vec([]byte{0x01, 0x04, 0x2a, 0x00, 0x00, 0x00})))
	}
	return wasmtest.Module(sections...)
}

func TestRV64RootedScalarMemoryExecutes(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedScalarMemoryModule(true, false, false, false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || view.memoryCount() != 1 || !view.HasMemory || view.memoryDef(0).Min != 1 || view.memoryDef(0).Max != 2 {
		t.Fatal("generic rooted memory receipt is missing")
	}
	candidate := cloneCompiledMetadata(view)
	directory := *candidate.memoryDir
	directory.defs = slices.Clone(candidate.memoryDir.defs)
	candidate.memoryDir = &directory
	candidate.memoryDir.defs[0].Max = 257
	if rv64FreshRootedGC(candidate) {
		t.Fatal("oversized rooted memory retained the generic receipt")
	}
	if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatalf("oversized rooted memory validation=%v", err)
	}
	for name, mutate := range map[string]func(*Compiled){
		"cached-min":        func(candidate *Compiled) { candidate.MemMinPages = 0 },
		"cached-max":        func(candidate *Compiled) { candidate.MemMaxPages = 1 },
		"cached-has-max":    func(candidate *Compiled) { candidate.MemHasMax = false },
		"missing-directory": func(candidate *Compiled) { candidate.memoryDir = nil },
		"declared-max": func(candidate *Compiled) {
			directory := *candidate.memoryDir
			directory.defs = slices.Clone(directory.defs)
			candidate.memoryDir = &directory
			candidate.memoryDir.defs[0].Max = 3
		},
		"declared-min-and-cache": func(candidate *Compiled) {
			directory := *candidate.memoryDir
			directory.defs = slices.Clone(directory.defs)
			candidate.memoryDir = &directory
			candidate.memoryDir.defs[0].Min = 0
			candidate.MemMinPages = 0
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("incoherent primary memory metadata retained the generic receipt")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatalf("incoherent primary memory metadata validation=%v", err)
			}
		})
	}
	for _, gc := range []GCConfig{{}, {CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}} {
		instance, err := Instantiate(compiled, InstantiateOptions{GC: gc})
		if err != nil {
			t.Fatal(err)
		}
		want := []uint64{142, 1, 1, 2}
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, want) {
			_ = instance.Close()
			t.Fatalf("run()=%v err=%v want=%v", got, err, want)
		}
		if err = instance.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRV64RootedScalarPinnedMemoryExecutes(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedScalarMemoryModule(false, false, false, false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || view.MemMinPages != 1 || view.MemMaxPages != 1 || !view.MemHasMax || view.memoryDef(0).Max != 2 {
		t.Fatal("pinned generic rooted memory receipt is missing")
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{142, 1}) {
		t.Fatalf("run()=%v err=%v want=[142 1]", got, err)
	}
}

func TestRV64RootedScalarActiveDataExecutes(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedScalarMemoryModule(false, true, false, false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Data) != 1 || view.Data[0].Offset.Base != 32 || !slices.Equal(view.Data[0].Bytes, []byte{42, 0, 0, 0}) {
		t.Fatal("active-data generic rooted receipt is missing")
	}
	for name, mutate := range map[string]func(*Compiled){
		"payload": func(candidate *Compiled) {
			candidate.Data = slices.Clone(candidate.Data)
			candidate.Data[0].Bytes = slices.Clone(candidate.Data[0].Bytes)
			candidate.Data[0].Bytes[0]++
		},
		"offset": func(candidate *Compiled) {
			candidate.Data = slices.Clone(candidate.Data)
			candidate.Data[0].Offset.Base++
		},
		"missing": func(candidate *Compiled) { candidate.Data = nil },
		"passive": func(candidate *Compiled) { candidate.PassiveData = []PassiveDataInit{{Bytes: []byte{1}}} },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated active data retained the generic receipt")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatalf("mutated active data validation=%v", err)
			}
		})
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{142, 1, 42}) {
		t.Fatalf("run()=%v err=%v want=[142 1 42]", got, err)
	}
}

func TestRV64RootedScalarMemoryExportExecutes(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedScalarMemoryModule(false, true, false, true))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || view.memoryDir == nil || len(view.memoryDir.exports) != 1 || view.memoryDir.exports["memory"] != 0 {
		t.Fatal("exported-memory generic rooted receipt is missing")
	}
	for name, mutate := range map[string]func(map[string]int){
		"removed": func(exports map[string]int) { delete(exports, "memory") },
		"renamed": func(exports map[string]int) {
			delete(exports, "memory")
			exports["renamed"] = 0
		},
		"bad-index": func(exports map[string]int) { exports["memory"] = 1 },
		"injected":  func(exports map[string]int) { exports["alias"] = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			directory := *candidate.memoryDir
			directory.exports = cloneExportMap(candidate.memoryDir.exports)
			candidate.memoryDir = &directory
			mutate(directory.exports)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated memory export namespace retained the generic receipt")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatalf("mutated memory export validation=%v", err)
			}
		})
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.ExportedMemory("missing"); err == nil {
		t.Fatal("undeclared memory export was accepted")
	}
	memory, err := instance.ExportedMemory("memory")
	if err != nil {
		t.Fatal(err)
	}
	if got := memory.UnsafeBytes()[32]; got != 42 {
		t.Fatalf("active data byte=%d want=42", got)
	}
	memory.UnsafeBytes()[32] = 77
	if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{142, 1, 77}) {
		t.Fatalf("run()=%v err=%v want=[142 1 77]", got, err)
	}
}

func rootedScalarGlobalModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}),
	}
	body := []byte{
		0x00,
		0x23, 0x00, 0x41, 0x01, 0x6a, 0x24, 0x00,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		0x23, 0x00, 0x23, 0x01, 0x23, 0x02, 0x23, 0x03, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x29, 0x0b}),
			wasmtest.GlobalEntry(wasm.I64, false, []byte{0x42, 0x7e, 0x0b}),
			wasmtest.GlobalEntry(wasm.F32, false, []byte{0x43, 0x34, 0x12, 0xc0, 0x7f, 0x0b}),
			wasmtest.GlobalEntry(wasm.F64, false, []byte{0x44, 0x78, 0x56, 0x00, 0x00, 0x00, 0x00, 0xf8, 0x7f, 0x0b}),
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("counter", byte(wasm.ExternGlobal), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedScalarGlobalExecutes(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedScalarGlobalModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Globals) != 4 || len(view.GlobalExports) != 1 || view.GlobalExports["counter"] != 0 {
		t.Fatal("numeric-global generic rooted receipt is missing")
	}
	for name, mutate := range map[string]func(*Compiled){
		"initial-bits": func(candidate *Compiled) {
			candidate.Globals = slices.Clone(candidate.Globals)
			candidate.Globals[0].Bits++
		},
		"mutability": func(candidate *Compiled) {
			candidate.Globals = slices.Clone(candidate.Globals)
			candidate.Globals[0].Mutable = false
		},
		"upper-bits": func(candidate *Compiled) {
			candidate.Globals = slices.Clone(candidate.Globals)
			candidate.Globals[0].Bits |= 1 << 32
		},
		"missing-feature": func(candidate *Compiled) {
			candidate.requiredFeatures &^= CoreFeatureMutableGlobal
		},
		"renamed-export": func(candidate *Compiled) {
			candidate.GlobalExports = cloneExportMap(candidate.GlobalExports)
			delete(candidate.GlobalExports, "counter")
			candidate.GlobalExports["renamed"] = 0
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated global metadata retained the generic receipt")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatalf("mutated global validation=%v", err)
			}
		})
	}
	for range 2 {
		instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := instance.Global("counter"); err != nil || got != 41 {
			_ = instance.Close()
			t.Fatalf("initial counter=%d err=%v want=41", got, err)
		}
		want := []uint64{42, ^uint64(1), 0x7fc01234, 0x7ff8000000005678}
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, want) {
			_ = instance.Close()
			t.Fatalf("run()=%v err=%v want=%v", got, err, want)
		}
		if err := instance.SetGlobal("counter", 0xffffffff00000064); err != nil {
			_ = instance.Close()
			t.Fatal(err)
		}
		want[0] = 101
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, want) {
			_ = instance.Close()
			t.Fatalf("run() after host set=%v err=%v want=%v", got, err, want)
		}
		if err := instance.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func rootedScalarStartModule(trap bool) []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, nil),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	start := []byte{
		0x00,
		0x41, 0x29, 0x24, 0x00,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
	}
	if trap {
		start = append(start, 0x00)
	}
	start = append(start, 0x0b)
	get := []byte{0x00, 0x23, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0x00, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("get", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("counter", byte(wasm.ExternGlobal), 0),
		)),
		wasmtest.Section(8, wasmtest.ULEB(0)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(start))), start...),
			append(wasmtest.ULEB(uint32(len(get))), get...),
		)),
	)
}

func TestRV64RootedScalarStartExecutes(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedScalarStartModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || !view.HasStart || view.StartIsImport || view.StartLocalFunc != 0 {
		t.Fatal("local-start generic rooted receipt is missing")
	}
	for name, mutate := range map[string]func(*Compiled){
		"missing":  func(candidate *Compiled) { candidate.HasStart = false },
		"imported": func(candidate *Compiled) { candidate.StartIsImport = true },
		"index":    func(candidate *Compiled) { candidate.StartLocalFunc = 1 },
		"signature": func(candidate *Compiled) {
			candidate.Funcs[0].Results = []ValType{ValI32}
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated start metadata retained the generic receipt")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatalf("mutated start validation=%v", err)
			}
		})
	}
	for range 2 {
		instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := instance.Invoke("get"); err != nil || !slices.Equal(got, []uint64{41}) {
			_ = instance.Close()
			t.Fatalf("get()=%v err=%v want=[41]", got, err)
		}
		if err := instance.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRV64RootedScalarStartTrapAbortsInstantiation(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedScalarStartModule(true))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}}); err == nil || instance != nil {
		if instance != nil {
			_ = instance.Close()
		}
		t.Fatalf("trapping start instantiated=%v err=%v", instance, err)
	}
}

func rootedScalarPassiveDataModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32}),
	}
	body := []byte{
		0x01, 0x01, 0x63, 0x00,
		// Keep an exact collector root live while a second allocation forces GC.
		0x41, 0x2a, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		// Copy passive data index 1 into memory 0, leaving active index 0 in
		// the same original data-index namespace.
		0x41, 0x20, 0x41, 0x00, 0x41, 0x04, 0xfc, 0x08, 0x01, 0x00,
		0x41, 0xc0, 0x00, 0x28, 0x02, 0x00,
		0x41, 0x20, 0x28, 0x02, 0x00,
		0xfc, 0x09, 0x01,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		// Data-count has its proposal-defined order before code, despite its
		// numeric section id following data.
		wasmtest.Section(12, wasmtest.ULEB(2)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
		wasmtest.Section(11, wasmtest.Vec(
			[]byte{0x00, 0x41, 0xc0, 0x00, 0x0b, 0x04, 0x07, 0x00, 0x00, 0x00},
			[]byte{0x01, 0x04, 0x2a, 0x00, 0x00, 0x00},
		)),
	)
}

func TestRV64RootedScalarPassiveDataExecutes(t *testing.T) {
	source := rootedScalarPassiveDataModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("bounded rooted passive-data module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Data) != 1 || len(view.PassiveData) != 2 || view.PassiveData[0].Bytes != nil || !slices.Equal(view.PassiveData[1].Bytes, []byte{42, 0, 0, 0}) {
		t.Fatal("passive-data generic rooted receipt is missing")
	}
	for name, mutate := range map[string]func(*Compiled){
		"payload": func(candidate *Compiled) {
			candidate.PassiveData = slices.Clone(candidate.PassiveData)
			candidate.PassiveData[1].Bytes = slices.Clone(candidate.PassiveData[1].Bytes)
			candidate.PassiveData[1].Bytes[0]++
		},
		"missing-slot": func(candidate *Compiled) { candidate.PassiveData = candidate.PassiveData[:1] },
		"wrong-index": func(candidate *Compiled) {
			candidate.PassiveData = slices.Clone(candidate.PassiveData)
			candidate.PassiveData[0], candidate.PassiveData[1] = candidate.PassiveData[1], candidate.PassiveData[0]
		},
		"missing-feature": func(candidate *Compiled) { candidate.requiredFeatures &^= CoreFeatureBulkMemoryOperations },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated passive data retained the generic receipt")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatalf("mutated passive data validation=%v", err)
			}
		})
	}

	first, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := first.Invoke("run"); err != nil || !slices.Equal(got, []uint64{7, 42}) {
		_ = first.Close()
		t.Fatalf("first run()=%v err=%v want=[7 42]", got, err)
	}
	if got, err := first.Invoke("run"); err == nil || got != nil {
		_ = first.Close()
		t.Fatalf("memory.init after data.drop returned %v err=%v", got, err)
	} else if trap, ok := err.(*TrapError); !ok || trap.Code != TrapLinMemOutOfBounds {
		_ = first.Close()
		t.Fatalf("memory.init after data.drop trap=%v, want %v", err, TrapLinMemOutOfBounds)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	// Dropping a segment mutates instance state only; a fresh instance gets a
	// fresh passive descriptor directory backed by the immutable compiled bytes.
	second, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if got, err := second.Invoke("run"); err != nil || !slices.Equal(got, []uint64{7, 42}) {
		t.Fatalf("fresh instance run()=%v err=%v want=[7 42]", got, err)
	}
}

func TestRV64RootedScalarInertPassiveDataExecutes(t *testing.T) {
	// A passive declaration itself requires bulk memory but does not require a
	// data-count section until an instruction addresses the data-index space.
	compiled, err := Compile(rv64RootedScalarConfig(), rootedScalarMemoryModule(false, false, true, false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.PassiveData) != 1 || !slices.Equal(view.PassiveData[0].Bytes, []byte{42, 0, 0, 0}) || !view.requiredFeatures.IsEnabled(CoreFeatureBulkMemoryOperations) {
		t.Fatal("inert passive-data generic rooted receipt is missing")
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{142, 1, 0}) {
		t.Fatalf("run()=%v err=%v want=[142 1 0]", got, err)
	}
}

func rootedScalarBoundaryStructModule(fieldCount int) []byte {
	fields := []byte{0x5f}
	fields = append(fields, wasmtest.ULEB(uint32(fieldCount))...)
	for field := range fieldCount {
		mut := byte(0)
		if field == fieldCount-1 {
			mut = 1
		}
		fields = append(fields, 0x7f, mut)
	}
	body := []byte{0x01, 0x01, 0x64, 0x01}
	for field := range fieldCount {
		body = append(body, 0x41)
		body = append(body, wasmtest.SLEB32(int32(field))...)
	}
	body = append(body, 0xfb, 0x00, 0x01, 0x21, 0x00, 0x20, 0x00, 0x41, 0x2a, 0xfb, 0x05, 0x01)
	body = append(body, wasmtest.ULEB(uint32(fieldCount-1))...)
	body = append(body, 0x20, 0x00, 0xfb, 0x02, 0x01)
	body = append(body, wasmtest.ULEB(uint32(fieldCount-1))...)
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), fields)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedScalarBoundaryStructsExecute(t *testing.T) {
	for _, fields := range []int{78, 255} {
		t.Run(fmt.Sprintf("fields=%d", fields), func(t *testing.T) {
			compiled, err := Compile(rv64RootedScalarConfig(), rootedScalarBoundaryStructModule(fields))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			if len(instance.serArgs) < (fields+1)*8 {
				t.Fatalf("generic struct constructor argument arena = %d bytes, want at least %d", len(instance.serArgs), (fields+1)*8)
			}
			got, err := instance.Invoke("run")
			if err != nil || !slices.Equal(got, []uint64{42}) {
				t.Fatalf("run()=%v err=%v want=[42]", got, err)
			}
		})
	}
}

func TestRV64RootedScalarStressGCLifetime(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), typedSelectReferenceModule(130, true, false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	rt := NewRuntime(WithRuntimeConfig(rv64RootedScalarConfig()))
	defer rt.Close()
	module, err := rt.Module(compiled)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := rt.Instantiate(context.Background(), module, WithImports(NewImports()), WithGC(GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}))
	module.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if instance.gc == nil || !instance.gc.CollectsOnAllocation() {
		t.Fatal("generic rooted scalar stress collector is not active")
	}
	for iteration := range 32 {
		condition := uint32(iteration & 1)
		want := uint64(22)
		if condition != 0 {
			want = 11
		}
		got, err := instance.Invoke("run", I32(int32(condition)))
		if err != nil || !slices.Equal(got, []uint64{want}) {
			t.Fatalf("iteration %d: run(%d)=%v err=%v want=[%d]", iteration, condition, got, err, want)
		}
		if err = instance.CollectGC(); err != nil {
			t.Fatalf("iteration %d collect: %v", iteration, err)
		}
	}
}

func TestRV64FreshRootedScalarReceipt(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), typedSelectReferenceModule(130, true, false))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) {
		t.Fatal("fresh rooted GC compiler receipt is missing")
	}
	if err := railshotValidateCompiled(view); err != nil {
		t.Fatal(err)
	}

	mutated := *view
	mutated.code = slices.Clone(view.code)
	mutated.code[len(mutated.code)-1] ^= 1
	if rv64FreshRootedGC(&mutated) {
		t.Fatal("mutated code retained the fresh compiler receipt")
	}
	if err := railshotValidateCompiled(&mutated); !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatalf("mutated code admission = %v", err)
	}

	blob, err := compiled.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var loaded Compiled
	if err := loaded.UnmarshalBinary(blob); err == nil {
		_ = loaded.Close()
		t.Fatal("generic rooted RV64 artifact unexpectedly retained a fresh compiler receipt")
	}
}
