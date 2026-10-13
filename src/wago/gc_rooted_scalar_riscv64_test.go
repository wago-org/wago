//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo && !wago_precompiled

package wago

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64RootedScalarConfig() *RuntimeConfig {
	return rv64I31Config().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTypedFunctionReferences)
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
