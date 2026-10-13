//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo && !wago_precompiled

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"runtime"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	native "github.com/wago-org/wago/src/core/runtime/gc/native"
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

func rootedScalarPublicReferenceModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x01},
		{0x60, 0x00, 0x01, 0x64, 0x00},
		{0x60, 0x01, 0x64, 0x00, 0x01, 0x7f},
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I31Ref}),
		wasmtest.FuncType([]wasm.ValType{wasm.I31Ref}, []wasm.ValType{wasm.I32}),
	}
	makeStruct := []byte{0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0x0b}
	readStruct := []byte{0x00, 0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	wrapI31 := []byte{0x00, 0x20, 0x00, 0xfb, 0x1c, 0x0b}
	unwrapI31 := []byte{0x00, 0x20, 0x00, 0xfb, 0x1d, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(4))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("make", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("read", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("wrap_i31", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("unwrap_i31", byte(wasm.ExternFunc), 3),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(makeStruct))), makeStruct...),
			append(wasmtest.ULEB(uint32(len(readStruct))), readStruct...),
			append(wasmtest.ULEB(uint32(len(wrapI31))), wrapI31...),
			append(wasmtest.ULEB(uint32(len(unwrapI31))), unwrapI31...),
		)),
	)
}

func TestRV64RootedReferenceExportsExecute(t *testing.T) {
	source := rootedScalarPublicReferenceModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("bounded public collector-reference module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || view.genericGCFrameRoots() == nil {
		t.Fatalf("public collector-reference receipt=%t roots=%+v validation=%v", rv64FreshRootedGC(view), view.genericGCFrameRoots(), railshotValidateCompiled(view))
	}
	candidate := cloneCompiledMetadata(view)
	candidate.Exports = make(map[string]int, len(view.Exports))
	for name, function := range view.Exports {
		candidate.Exports[name] = function
	}
	delete(candidate.Exports, "make")
	candidate.Exports["renamed"] = 0
	if rv64FreshRootedGC(candidate) {
		t.Fatal("mutated public function namespace retained the rooted compiler receipt")
	}

	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	values, err := in.InvokeValues(context.Background(), "make")
	if err != nil || len(values) != 1 || values[0].Type() != ValAnyRef || values[0].GCRef().IsNull() || values[0].Bits()>>32 == 0 {
		t.Fatalf("make()=%v err=%v; want one opaque aggregate token", values, err)
	}
	ref := values[0].GCRef()
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	if got, err := in.InvokeValues(context.Background(), "read", ValueGCRef(ref)); err != nil || len(got) != 1 || got[0].I32() != 42 {
		t.Fatalf("typed read(token)=%v err=%v; want i32(42)", got, err)
	}
	if got, err := in.Invoke("read", values[0].Bits()); err != nil || !slices.Equal(got, []uint64{42}) {
		t.Fatalf("raw read(token)=%v err=%v; want [42]", got, err)
	}
	if got, err := in.Invoke("wrap_i31", 0xdeadbeeffffffffe); err != nil || !slices.Equal(got, []uint64{0xfffffffd}) {
		t.Fatalf("wrap_i31(dirty -2)=%#x err=%v; want [0xfffffffd]", got, err)
	}
	wrapped, err := in.InvokeValues(context.Background(), "wrap_i31", ValueI32(-2))
	if err != nil || len(wrapped) != 1 || wrapped[0].Type() != ValI31Ref || wrapped[0].I31Ref().Signed() != -2 {
		t.Fatalf("typed wrap_i31(-2)=%v err=%v", wrapped, err)
	}
	if got, err := in.Invoke("unwrap_i31", wrapped[0].Bits()); err != nil || !slices.Equal(got, []uint64{0xfffffffe}) {
		t.Fatalf("unwrap_i31(-2)=%#x err=%v; want zero-normalized i32 -2", got, err)
	}
	if err := in.ReleaseGCRef(ref); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Invoke("read", values[0].Bits()); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale aggregate token ingress=%v", err)
	}
}

func rootedScalarReferenceHostImportModule() []byte {
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

func TestRV64RootedReferenceHostImportExecutes(t *testing.T) {
	source := rootedScalarReferenceHostImportModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("bounded collector-reference host import was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	roots := view.genericGCFrameRoots()
	if !rv64FreshRootedGC(view) || roots == nil || len(roots.callsites) != 1 || len(roots.callsites[0].offsets) != 1 {
		t.Fatalf("reference host receipt=%t roots=%+v validation=%v", rv64FreshRootedGC(view), roots, railshotValidateCompiled(view))
	}
	candidate := cloneCompiledMetadata(view)
	candidate.importFuncSigs = slices.Clone(candidate.importFuncSigs)
	candidate.importFuncSigs[0].Params = slices.Clone(candidate.importFuncSigs[0].Params)
	candidate.importFuncSigs[0].Params[0] = ValI31Ref
	if rv64FreshRootedGC(candidate) {
		t.Fatal("mutated collector-reference host ABI retained the rooted compiler receipt")
	}

	rt := NewRuntime(WithRuntimeConfig(rv64RootedScalarConfig()))
	defer rt.Close()
	var calls int
	host, err := rt.NewGCHostFuncRef(slotHostFunc(func(module HostModule, params, results []uint64) {
		calls++
		if len(params) != 1 || len(results) != 1 || params[0] == 0 || params[0] == uint64(uint32(params[0])) {
			panic("collector-reference host argument is not an opaque token")
		}
		gcHost, ok := module.(GCHostModule)
		if !ok {
			panic("collector-reference host call has no GC surface")
		}
		if err := gcHost.CollectGC(); err != nil {
			panic(err)
		}
		results[0] = params[0]
	}), FuncSig{Params: []ValType{ValAnyRef}, Results: []ValType{ValAnyRef}})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	module, err := rt.Module(compiled)
	if err != nil {
		t.Fatal(err)
	}
	in, err := rt.Instantiate(context.Background(), module, WithImports(testImports("env.echo", host)), WithGC(GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}))
	module.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for range 8 {
		if got, err := in.Invoke("run"); err != nil || !slices.Equal(got, []uint64{42}) {
			t.Fatalf("run()=%v err=%v want=[42]", got, err)
		}
	}
	if calls != 8 {
		t.Fatalf("collector-reference host calls=%d want=8", calls)
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

func rootedScalarI31LocalModule() []byte {
	body := []byte{
		0x01, 0x01, 0x6c,
		0x41, 0x7f, 0xfb, 0x1c, 0x21, 0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00, 0x1a,
		0x20, 0x00, 0xfb, 0x1e,
		0x20, 0x00, 0xfb, 0x1d,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedScalarI31LocalSurvivesGC(t *testing.T) {
	source := rootedScalarI31LocalModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("rooted i31 local module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	roots := view.genericGCFrameRoots()
	if !rv64FreshRootedGC(view) || roots == nil || len(roots.safepoints) != 1 || len(roots.safepoints[0].offsets) != 1 || roots.safepoints[0].offsets[0] != shared.RV64CallFrameHeaderBytes {
		t.Fatalf("rooted i31 receipt=%t roots=%+v validation=%v", rv64FreshRootedGC(view), roots, railshotValidateCompiled(view))
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for iteration := range 16 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{0x7fffffff, 0xffffffff}) {
			t.Fatalf("iteration %d: run()=%#x err=%v", iteration, got, err)
		}
	}
}

func rootedScalarI31InternalCallModule() []byte {
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

func TestRV64RootedScalarI31InternalCallSurvivesGC(t *testing.T) {
	source := rootedScalarI31InternalCallModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("rooted i31 internal call module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	roots := view.genericGCFrameRoots()
	if !rv64FreshRootedGC(view) || roots == nil || len(roots.safepoints) != 1 || len(roots.safepoints[0].offsets) != 1 || len(roots.callsites) != 2 {
		t.Fatalf("rooted i31 call receipt=%t roots=%+v validation=%v", rv64FreshRootedGC(view), roots, railshotValidateCompiled(view))
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for iteration := range 16 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{0x7fffffff, 0xffffffff}) {
			t.Fatalf("iteration %d: run()=%#x err=%v", iteration, got, err)
		}
	}
}

func rootedScalarI31StructModule() []byte {
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

func TestRV64RootedScalarI31StructSurvivesGC(t *testing.T) {
	source := rootedScalarI31StructModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("rooted i31 struct module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	roots := view.genericGCFrameRoots()
	if !rv64FreshRootedGC(view) || roots == nil || len(roots.safepoints) != 2 || len(roots.safepoints[1].offsets) != 1 {
		t.Fatalf("rooted i31 struct receipt=%t roots=%+v validation=%v", rv64FreshRootedGC(view), roots, railshotValidateCompiled(view))
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for iteration := range 16 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{0x7ffffffe, 0xfffffffe}) {
			t.Fatalf("iteration %d: run()=%#x err=%v", iteration, got, err)
		}
	}
}

func rootedScalarI31ArrayModule() []byte {
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

func TestRV64RootedScalarI31ArraySurvivesGC(t *testing.T) {
	source := rootedScalarI31ArrayModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("rooted i31 array module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	roots := view.genericGCFrameRoots()
	if !rv64FreshRootedGC(view) || roots == nil || len(roots.safepoints) != 2 || len(roots.safepoints[1].offsets) != 1 {
		t.Fatalf("rooted i31 array receipt=%t roots=%+v validation=%v", rv64FreshRootedGC(view), roots, railshotValidateCompiled(view))
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for iteration := range 16 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{0x7ffffffe, 0xfffffffe}) {
			t.Fatalf("iteration %d: run()=%#x err=%v", iteration, got, err)
		}
	}
}

func rootedScalarReferenceControlModule() []byte {
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

func TestRV64RootedScalarReferenceControlExecutes(t *testing.T) {
	source := rootedScalarReferenceControlModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("rooted reference control module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) {
		t.Fatalf("rooted reference control receipt=false validation=%v", railshotValidateCompiled(view))
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for iteration := range 16 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{1, 1, 1, 9}) {
			t.Fatalf("iteration %d: run()=%v err=%v", iteration, got, err)
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

func rootedScalarI31TestCastModule() []byte {
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

func TestRV64RootedScalarI31TestCastExecutes(t *testing.T) {
	source := rootedScalarI31TestCastModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("rooted i31 ref.test/ref.cast module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) {
		t.Fatalf("rooted i31 ref.test/ref.cast receipt=false validation=%v", railshotValidateCompiled(view))
	}
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for iteration := range 16 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{1, 0, 9, 1}) {
			t.Fatalf("iteration %d: run()=%v err=%v", iteration, got, err)
		}
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

func rootedScalarMultiMemoryModule(imported bool) []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
	}
	size1 := []byte{0x00, 0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a, 0x3f, 0x01, 0x0b}
	store1 := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x36, 0x42, 0x01, 0x00, 0x0b}
	load1 := []byte{0x00, 0x20, 0x00, 0x28, 0x42, 0x01, 0x00, 0x0b}
	load0 := []byte{0x00, 0x20, 0x00, 0x28, 0x02, 0x00, 0x0b}
	data1 := []byte{0x02, 0x01, 0x41, 0x10, 0x0b, 0x04, 0x78, 0x56, 0x34, 0x12}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(types...)),
	}
	if imported {
		memoryImport := func(name string, min, max byte) []byte {
			entry := append(append(wasmtest.Name("env.memories"), wasmtest.Name(name)...), byte(wasm.ExternMem))
			return append(entry, 0x01, min, max)
		}
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(
			memoryImport("memory.0", 1, 1),
			memoryImport("memory.1", 2, 2),
		)))
	}
	sections = append(sections,
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(3))),
	)
	if !imported {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec(
			[]byte{0x01, 0x01, 0x01},
			[]byte{0x01, 0x02, 0x02},
		)))
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("size1", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("store1", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("load1", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("load0", byte(wasm.ExternFunc), 3),
			wasmtest.ExportEntry("m0", byte(wasm.ExternMem), 0),
			wasmtest.ExportEntry("m1", byte(wasm.ExternMem), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(size1))), size1...),
			append(wasmtest.ULEB(uint32(len(store1))), store1...),
			append(wasmtest.ULEB(uint32(len(load1))), load1...),
			append(wasmtest.ULEB(uint32(len(load0))), load0...),
		)),
		wasmtest.Section(11, wasmtest.Vec(data1)),
	)
	return wasmtest.Module(sections...)
}

func rootedScalarImportedMemoryModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}),
	}
	memoryImport := append(append(wasmtest.Name("env.memories"), wasmtest.Name("memory.0")...), byte(wasm.ExternMem), 0x01, 0x01, 0x02)
	body := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		0x41, 0x10, 0x41, 0xe4, 0x00, 0x36, 0x02, 0x00,
		0x41, 0x10, 0x28, 0x02, 0x00,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x6a,
		0x3f, 0x00,
		0x41, 0x20, 0x28, 0x02, 0x00,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("memory", byte(wasm.ExternMem), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
		wasmtest.Section(11, wasmtest.Vec([]byte{0x00, 0x41, 0x20, 0x0b, 0x04, 0x2a, 0x00, 0x00, 0x00})),
	)
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

func TestRV64RootedScalarMultiMemoryExecutes(t *testing.T) {
	source := rootedScalarMultiMemoryModule(false)
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("bounded rooted multi-memory module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig().WithFeature(CoreFeatureMultiMemory, true), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || view.memoryCount() != 2 || view.memoryDir == nil || !view.memoryDir.staged || len(view.Data) != 1 || view.Data[0].MemoryIndex != 1 ||
		view.memoryExportMap()["m0"] != 0 || view.memoryExportMap()["m1"] != 1 || !view.requiredFeatures.IsEnabled(CoreFeatureMultiMemory) {
		t.Fatalf("rooted multi-memory receipt=%t memories=%d directory=%+v data=%+v exports=%v features=%v validation=%v", rv64FreshRootedGC(view), view.memoryCount(), view.memoryDir, view.Data, view.memoryExportMap(), view.requiredFeatures, railshotValidateCompiled(view))
	}
	for name, mutate := range map[string]func(*Compiled){
		"definition": func(candidate *Compiled) {
			directory := *candidate.memoryDir
			directory.defs = slices.Clone(directory.defs)
			candidate.memoryDir = &directory
			candidate.memoryDir.defs[1].Max++
		},
		"data-target": func(candidate *Compiled) {
			candidate.Data = slices.Clone(candidate.Data)
			candidate.Data[0].MemoryIndex = 0
		},
		"export-target": func(candidate *Compiled) {
			directory := *candidate.memoryDir
			directory.exports = cloneExportMap(directory.exports)
			candidate.memoryDir = &directory
			candidate.memoryDir.exports["m1"] = 0
		},
		"feature": func(candidate *Compiled) {
			candidate.requiredFeatures &^= CoreFeatureMultiMemory
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated multi-memory metadata retained the generic receipt")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatalf("mutated multi-memory validation=%v", err)
			}
		})
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for range 4 {
		if got, err := instance.Invoke("size1"); err != nil || !slices.Equal(got, []uint64{2}) {
			t.Fatalf("size1()=%v err=%v want=[2]", got, err)
		}
	}
	if got, err := instance.Invoke("load1", 16); err != nil || !slices.Equal(got, []uint64{0x12345678}) {
		t.Fatalf("active memory-1 data=%v err=%v want=[0x12345678]", got, err)
	}
	if _, err := instance.Invoke("store1", 0xdeadbeef00000020, 0xfeedface12345678); err != nil {
		t.Fatal(err)
	}
	if got, err := instance.Invoke("load1", 0xbaadf00d00000020); err != nil || !slices.Equal(got, []uint64{0x12345678}) {
		t.Fatalf("memory-1 load=%v err=%v want=[0x12345678]", got, err)
	}
	if got, err := instance.Invoke("load0", 0xbaadf00d00000020); err != nil || !slices.Equal(got, []uint64{0}) {
		t.Fatalf("memory-0 isolation load=%v err=%v want=[0]", got, err)
	}
	memory1, err := instance.ExportedMemory("m1")
	if err != nil || memory1 == nil || len(memory1.UnsafeBytes()) != 2*65536 {
		t.Fatalf("memory-1 export=%p bytes=%d err=%v", memory1, func() int {
			if memory1 == nil {
				return 0
			}
			return len(memory1.UnsafeBytes())
		}(), err)
	}
}

func TestRV64RootedScalarImportedMultiMemoryExecutes(t *testing.T) {
	source := rootedScalarMultiMemoryModule(true)
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("bounded rooted imported multi-memory module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig().WithFeature(CoreFeatureMultiMemory, true), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || view.memoryCount() != 2 || view.memoryImportCount() != 2 || view.memoryDef(0).ImportKey == "" || view.memoryDef(1).ImportKey == "" {
		t.Fatalf("rooted imported multi-memory receipt=%t memories=%d imports=%d defs=%+v validation=%v", rv64FreshRootedGC(view), view.memoryCount(), view.memoryImportCount(), view.memoryDir.defs, railshotValidateCompiled(view))
	}
	candidate := cloneCompiledMetadata(view)
	directory := *candidate.memoryDir
	directory.defs = slices.Clone(directory.defs)
	candidate.memoryDir = &directory
	candidate.memoryDir.defs[1].ImportKey = "env.memories.other"
	if rv64FreshRootedGC(candidate) {
		t.Fatal("renamed secondary memory import retained the generic receipt")
	}
	if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatalf("renamed secondary memory validation=%v", err)
	}
	memory0, err := NewMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer memory0.Close()
	memory1, err := NewMemory(2, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer memory1.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{
		Imports: NewImports().Memory("env.memories", "memory.0", memory0).Memory("env.memories", "memory.1", memory1),
		GC:      GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if exported, err := instance.ExportedMemory("m0"); err != nil || exported != memory0 {
		t.Fatalf("memory-0 export=%p want=%p err=%v", exported, memory0, err)
	}
	if exported, err := instance.ExportedMemory("m1"); err != nil || exported != memory1 {
		t.Fatalf("memory-1 export=%p want=%p err=%v", exported, memory1, err)
	}
	for range 4 {
		if got, err := instance.Invoke("size1"); err != nil || !slices.Equal(got, []uint64{2}) {
			t.Fatalf("size1()=%v err=%v want=[2]", got, err)
		}
	}
	if got, err := instance.Invoke("load1", 16); err != nil || !slices.Equal(got, []uint64{0x12345678}) {
		t.Fatalf("imported memory-1 active data=%v err=%v want=[0x12345678]", got, err)
	}
	if _, err := instance.Invoke("store1", 32, 0x76543210); err != nil {
		t.Fatal(err)
	}
	if got, err := instance.Invoke("load1", 32); err != nil || !slices.Equal(got, []uint64{0x76543210}) {
		t.Fatalf("imported memory-1 load=%v err=%v want=[0x76543210]", got, err)
	}
	if got, err := instance.Invoke("load0", 32); err != nil || !slices.Equal(got, []uint64{0}) {
		t.Fatalf("imported memory-0 isolation load=%v err=%v want=[0]", got, err)
	}
}

func TestRV64RootedScalarImportedMemoryExecutes(t *testing.T) {
	source := rootedScalarImportedMemoryModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("bounded rooted imported memory was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || view.memoryCount() != 1 || view.memoryImport == "" || view.memoryDef(0).ImportKey != view.memoryImport {
		exportDigest, exportValid := rv64RootedMemoryExportDigest(view)
		dataDigest, dataValid := rv64RootedActiveDataDigest(view)
		globalDigest, globalValid := rv64RootedGlobalDigest(view)
		tableDigest, tableValid := rv64RootedTableDigest(view)
		importDigest, importValid := rv64RootedImportDigest(view)
		t.Fatalf("rooted imported-memory receipt=%t count=%d key=%q/%q export=%t/%t data=%t/%t global=%t/%t table=%t/%t import=%t/%t has=%t minmax=%d/%d/%t dir=%+v fields=%+v roots=%v validation=%v", rv64FreshRootedGC(view), view.memoryCount(), view.memoryImport, view.memoryDef(0).ImportKey, exportValid, exportDigest == view.validateMemo.rv64FreshRootedGCExportDigest, dataValid, dataDigest == view.validateMemo.rv64FreshRootedGCDataDigest, globalValid, globalDigest == view.validateMemo.rv64FreshRootedGCGlobalDigest, tableValid, tableDigest == view.validateMemo.rv64FreshRootedGCTableDigest, importValid, importDigest == view.validateMemo.rv64FreshRootedGCImportDigest, view.HasMemory, view.MemMinPages, view.MemMaxPages, view.MemHasMax, view.memoryDir, view.memoryDef(0), validateCompiledGCFrameRoots(view, view.genericGCFrameRoots()), railshotValidateCompiled(view))
	}
	for name, mutate := range map[string]func(*Compiled){
		"key": func(candidate *Compiled) {
			directory := *candidate.memoryDir
			directory.defs = slices.Clone(directory.defs)
			directory.defs[0].ImportKey = "env.other.memory.0"
			candidate.memoryDir = &directory
			candidate.memoryImport = directory.defs[0].ImportKey
		},
		"separator": func(candidate *Compiled) {
			memo := *candidate.loadValidateMemo()
			memo.importModuleEnds = slices.Clone(memo.importModuleEnds)
			memo.importModuleEnds[0]++
			candidate.validateMemo = &memo
		},
		"staged": func(candidate *Compiled) {
			directory := *candidate.memoryDir
			directory.staged = !directory.staged
			candidate.memoryDir = &directory
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated imported memory retained the generic receipt")
			}
		})
	}
	memory, err := NewMemory(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{
		Imports: NewImports().Memory("env.memories", "memory.0", memory),
		GC:      GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{142, 1, 42}) {
		t.Fatalf("run()=%v err=%v want=[142 1 42]", got, err)
	}
	if exported, err := instance.ExportedMemory("memory"); err != nil || exported != memory {
		t.Fatalf("imported memory re-export lost identity: exported=%p want=%p err=%v", exported, memory, err)
	}
	unstaged, err := Compile(rv64RootedScalarConfig().WithFeature(CoreFeatureMultiMemory, false), source)
	if err != nil {
		t.Fatal(err)
	}
	defer unstaged.Close()
	unstagedView := unstaged.executionView()
	if !rv64FreshRootedGC(unstagedView) || unstagedView.memoryDir.staged || unstagedView.validateMemo.rv64FreshRootedGCMemoryStaged {
		t.Fatalf("MVP imported memory lost its exact unstaged receipt: fresh=%t staged=%t/%t", rv64FreshRootedGC(unstagedView), unstagedView.memoryDir.staged, unstagedView.validateMemo.rv64FreshRootedGCMemoryStaged)
	}
	mvpMemory, err := NewMemory(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer mvpMemory.Close()
	mvpInstance, err := Instantiate(unstaged, InstantiateOptions{
		Imports: NewImports().Memory("env.memories", "memory.0", mvpMemory),
		GC:      GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer mvpInstance.Close()
	if got, err := mvpInstance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{142, 1, 42}) {
		t.Fatalf("MVP run()=%v err=%v want=[142 1 42]", got, err)
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

func rootedScalarGlobalCopyModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64, wasm.I32}),
	}
	body := []byte{
		0x00,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		0x23, 0x04, 0x23, 0x05, 0x23, 0x06, 0x23, 0x07, 0x23, 0x08,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(
			wasmtest.GlobalImportEntry("env", "i32", wasm.I32, false),
			wasmtest.GlobalImportEntry("env", "i64", wasm.I64, false),
			wasmtest.GlobalImportEntry("env", "f32", wasm.F32, false),
			wasmtest.GlobalImportEntry("env", "f64", wasm.F64, false),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.I32, false, []byte{0x23, 0x00, 0x0b}),
			wasmtest.GlobalEntry(wasm.I64, false, []byte{0x23, 0x01, 0x0b}),
			wasmtest.GlobalEntry(wasm.F32, false, []byte{0x23, 0x02, 0x0b}),
			wasmtest.GlobalEntry(wasm.F64, false, []byte{0x23, 0x03, 0x0b}),
			wasmtest.GlobalEntry(wasm.I32, true, []byte{0x23, 0x04, 0x0b}),
		)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rootedScalarIntegerGlobalExpressionModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I64}),
	}
	body := []byte{
		0x00,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		0x23, 0x02, 0x23, 0x03,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(
			wasmtest.GlobalImportEntry("env", "i32", wasm.I32, false),
			wasmtest.GlobalImportEntry("env", "i64", wasm.I64, false),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(
			wasmtest.GlobalEntry(wasm.I32, false, []byte{0x23, 0x00, 0x41, 0x02, 0x6c, 0x41, 0x01, 0x6a, 0x0b}),
			wasmtest.GlobalEntry(wasm.I64, true, []byte{0x23, 0x01, 0x42, 0x03, 0x7c, 0x0b}),
		)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
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

func TestRV64RootedScalarGlobalCopiesExecute(t *testing.T) {
	source := rootedScalarGlobalCopyModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("rooted numeric global-copy module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.GlobalImports) != 4 || len(view.Globals) != 9 {
		t.Fatalf("rooted global-copy receipt=%t imports=%d globals=%d validation=%v", rv64FreshRootedGC(view), len(view.GlobalImports), len(view.Globals), railshotValidateCompiled(view))
	}
	for i, want := range []int{0, 1, 2, 3, 4} {
		global := &view.Globals[4+i]
		if !global.HasInitGlobal || global.InitGlobal != want || global.Bits != 0 || len(global.InitExpr) != 0 {
			t.Fatalf("global %d initializer=%+v want parent %d", 4+i, global, want)
		}
	}
	for name, mutate := range map[string]func(*Compiled){
		"parent": func(candidate *Compiled) {
			candidate.Globals = slices.Clone(candidate.Globals)
			candidate.Globals[8].InitGlobal = 5
		},
		"self": func(candidate *Compiled) {
			candidate.Globals = slices.Clone(candidate.Globals)
			candidate.Globals[8].InitGlobal = 8
		},
		"bits": func(candidate *Compiled) {
			candidate.Globals = slices.Clone(candidate.Globals)
			candidate.Globals[8].Bits = 1
		},
		"feature": func(candidate *Compiled) {
			candidate.requiredFeatures &^= CoreFeatureExtendedConstExpressions
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated global-copy metadata retained the generic receipt")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatalf("mutated global-copy validation=%v", err)
			}
		})
	}
	i32, i64 := NewGlobalI32(41, false), NewGlobalI64(-2, false)
	f32, f64 := NewGlobalF32(1.5, false), NewGlobalF64(-2.25, false)
	defer i32.Close()
	defer i64.Close()
	defer f32.Close()
	defer f64.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{
		Imports: NewImports().Global("env", "i32", i32).Global("env", "i64", i64).Global("env", "f32", f32).Global("env", "f64", f64),
		GC:      GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	want := []uint64{41, ^uint64(1), uint64(math.Float32bits(1.5)), math.Float64bits(-2.25), 41}
	for range 4 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, want) {
			t.Fatalf("run()=%v err=%v want=%v", got, err, want)
		}
	}
}

func TestRV64RootedScalarIntegerGlobalExpressionsExecute(t *testing.T) {
	source := rootedScalarIntegerGlobalExpressionModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("rooted integer global-expression module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.GlobalImports) != 2 || len(view.Globals) != 4 || !view.requiredFeatures.IsEnabled(CoreFeatureExtendedConst) {
		t.Fatalf("rooted integer-expression receipt=%t imports=%d globals=%d features=%v validation=%v", rv64FreshRootedGC(view), len(view.GlobalImports), len(view.Globals), view.requiredFeatures, railshotValidateCompiled(view))
	}
	for i := 2; i < 4; i++ {
		global := &view.Globals[i]
		if len(global.InitExpr) == 0 || global.HasInitGlobal || global.InitGlobal != 0 {
			t.Fatalf("global %d expression initializer=%+v", i, global)
		}
	}
	for name, mutate := range map[string]func(*Compiled){
		"expression": func(candidate *Compiled) {
			candidate.Globals = slices.Clone(candidate.Globals)
			candidate.Globals[2].InitExpr = slices.Clone(candidate.Globals[2].InitExpr)
			candidate.Globals[2].InitExpr[2] = 0x42
		},
		"scope": func(candidate *Compiled) {
			candidate.Globals = slices.Clone(candidate.Globals)
			candidate.Globals[2].InitExpr = []byte{0x23, 0x02, 0x0b}
		},
		"bits": func(candidate *Compiled) {
			candidate.Globals = slices.Clone(candidate.Globals)
			candidate.Globals[2].Bits++
		},
		"feature": func(candidate *Compiled) {
			candidate.requiredFeatures &^= CoreFeatureExtendedConst
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated integer-expression metadata retained the generic receipt")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatalf("mutated integer-expression validation=%v", err)
			}
		})
	}
	i32, i64 := NewGlobalI32(41, false), NewGlobalI64(-2, false)
	defer i32.Close()
	defer i64.Close()
	binary.LittleEndian.PutUint64(i32.cell, 0xfeedface00000029)
	instance, err := Instantiate(compiled, InstantiateOptions{
		Imports: NewImports().Global("env", "i32", i32).Global("env", "i64", i64),
		GC:      GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for range 4 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{83, 1}) {
			t.Fatalf("run()=%v err=%v want=[83 1]", got, err)
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

func rootedScalarAggregateTableModule(array, exportTable bool) []byte {
	aggregate := []byte{0x5f, 0x01, 0x7f, 0x01}
	otherAggregate := []byte{0x5e, 0x7e, 0x01}
	newValue := []byte{0x41, 0x2a, 0xfb, 0x00, 0x00}
	newGarbage := []byte{0x41, 0x07, 0xfb, 0x00, 0x00}
	readValue := []byte{0x41, 0x00, 0x25, 0x00, 0xfb, 0x02, 0x00, 0x00}
	if array {
		aggregate = []byte{0x5e, 0x7f, 0x01}
		otherAggregate = []byte{0x5f, 0x01, 0x7e, 0x01}
		newValue = []byte{0x41, 0x2a, 0x41, 0x01, 0xfb, 0x06, 0x00}
		newGarbage = []byte{0x41, 0x07, 0x41, 0x01, 0xfb, 0x06, 0x00}
		readValue = []byte{0x41, 0x00, 0x25, 0x00, 0x41, 0x00, 0xfb, 0x0b, 0x00}
	}
	body := []byte{0x00, 0x41, 0x00}
	body = append(body, newValue...)
	body = append(body, 0x26, 0x00)
	body = append(body, newGarbage...)
	body = append(body, 0x1a)
	body = append(body, readValue...)
	body = append(body, 0x0b)
	exports := [][]byte{wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0)}
	if exportTable {
		exports = append(exports, wasmtest.ExportEntry("objects", byte(wasm.ExternTable), 0))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(aggregate, otherAggregate, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		// One private bounded table of nullable references to aggregate type 0.
		wasmtest.Section(4, []byte{0x01, 0x63, 0x00, 0x01, 0x01, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rootedScalarSharedAggregateTableModule(imported bool) []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	setType := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	readType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	growType := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	tableType := []byte{0x63, 0x00, 0x01, 0x01, 0x02}
	setBody := []byte{
		0x00,
		0x20, 0x00,
		0x20, 0x01, 0xfb, 0x00, 0x00,
		0x26, 0x00,
		0x41, 0x09, 0xfb, 0x00, 0x00, 0x1a,
		0x20, 0x00, 0x25, 0x00,
		0xfb, 0x02, 0x00, 0x00,
		0x0b,
	}
	readBody := []byte{0x00, 0x20, 0x00, 0x25, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	growBody := []byte{0x00, 0xd0, 0x00, 0x20, 0x00, 0xfc, 0x0f, 0x00, 0x0b}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(structType, setType, readType, growType))}
	if imported {
		entry := append(append(wasmtest.Name("provider"), wasmtest.Name("objects")...), byte(wasm.ExternTable))
		entry = append(entry, tableType...)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(entry)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3))))
	if !imported {
		sections = append(sections, wasmtest.Section(4, wasmtest.Vec(tableType)))
	}
	exports := [][]byte{
		wasmtest.ExportEntry("set", byte(wasm.ExternFunc), 0),
		wasmtest.ExportEntry("read", byte(wasm.ExternFunc), 1),
		wasmtest.ExportEntry("grow", byte(wasm.ExternFunc), 2),
	}
	if !imported {
		exports = append(exports, wasmtest.ExportEntry("objects", byte(wasm.ExternTable), 0))
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(setBody))), setBody...),
			append(wasmtest.ULEB(uint32(len(readBody))), readBody...),
			append(wasmtest.ULEB(uint32(len(growBody))), growBody...),
		)),
	)
	return wasmtest.Module(sections...)
}

func TestRV64RootedAggregateTablesExecute(t *testing.T) {
	for _, tc := range []struct {
		name  string
		array bool
	}{{name: "struct"}, {name: "array", array: true}} {
		t.Run(tc.name, func(t *testing.T) {
			source := rootedScalarAggregateTableModule(tc.array, false)
			if !railshot.AdmitGCRootedScalarSource(source) {
				t.Fatal("private aggregate-reference table was not source-admitted")
			}
			compiled, err := Compile(rv64RootedScalarConfig(), source)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			view := compiled.executionView()
			if !rv64FreshRootedGC(view) || view.tableCount() != 1 || view.tableElementType(0) != ValAnyRef || view.tableEntryBytes(0) != 8 {
				t.Fatalf("aggregate-table receipt=%t tables=%d type=%v stride=%d validation=%v", rv64FreshRootedGC(view), view.tableCount(), view.tableElementType(0), view.tableEntryBytes(0), railshotValidateCompiled(view))
			}
			candidate := cloneCompiledMetadata(view)
			candidate.ValueTypes = slices.Clone(view.ValueTypes)
			candidate.ValueTypes[view.tableDef(0).ValueTypeIndex].Ref.Heap.TypeIndex = 1
			if rv64FreshRootedGC(candidate) {
				t.Fatal("nominal aggregate-table retarget retained the rooted receipt")
			}
			instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			for iteration := range 16 {
				if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{42}) {
					t.Fatalf("iteration %d: run()=%v err=%v want=[42]", iteration, got, err)
				}
			}
		})
	}
}

func TestRV64RootedAggregateTableExportAdmitted(t *testing.T) {
	if !railshot.AdmitGCRootedScalarSource(rootedScalarAggregateTableModule(false, true)) {
		t.Fatal("collector-reference table export was not source-admitted")
	}
}

func TestRV64RootedSharedAggregateTableExecutes(t *testing.T) {
	providerSource := rootedScalarSharedAggregateTableModule(false)
	consumerSource := rootedScalarSharedAggregateTableModule(true)
	if !railshot.AdmitGCRootedScalarSource(providerSource) || !railshot.AdmitGCRootedScalarSource(consumerSource) {
		t.Fatal("shared collector-reference table modules were not source-admitted")
	}
	providerBase, err := Compile(rv64RootedScalarConfig(), providerSource)
	if err != nil {
		t.Fatal(err)
	}
	defer providerBase.Close()
	consumerBase, err := Compile(rv64RootedScalarConfig(), consumerSource)
	if err != nil {
		t.Fatal(err)
	}
	defer consumerBase.Close()
	if providerView, consumerView := providerBase.executionView(), consumerBase.executionView(); !rv64FreshRootedGC(providerView) || !rv64FreshRootedGC(consumerView) ||
		providerView.tableExports["objects"] != 0 || consumerView.tableImportCount() != 1 ||
		providerView.tableEntryBytes(0) != 8 || consumerView.tableEntryBytes(0) != 8 {
		t.Fatalf("shared table receipts provider=%t consumer=%t exports=%v imports=%d", rv64FreshRootedGC(providerView), rv64FreshRootedGC(consumerView), providerView.tableExports, consumerView.tableImportCount())
	}
	mutated := cloneCompiledMetadata(consumerBase.executionView())
	mutated.tableImportMin++
	if rv64FreshRootedGC(mutated) {
		t.Fatal("mutated rooted-table import retained the generic receipt")
	}

	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			providerCode, consumerCode := providerBase, consumerBase
			if artifact {
				providerCode = publicArtifactRoundTrip(t, providerBase)
				consumerCode = publicArtifactRoundTrip(t, consumerBase)
				defer providerCode.Close()
				defer consumerCode.Close()
			}
			rt := NewRuntime(WithRuntimeConfig(rv64RootedScalarConfig()))
			defer rt.Close()
			gcConfig := GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}
			providerModule, err := rt.Module(providerCode)
			if err != nil {
				t.Fatal(err)
			}
			provider, err := rt.Instantiate(context.Background(), providerModule, WithGC(gcConfig))
			providerModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			table, err := provider.ExportedTable("objects")
			if err != nil {
				provider.Close()
				t.Fatal(err)
			}
			consumerModule, err := rt.Module(consumerCode)
			if err != nil {
				provider.Close()
				t.Fatal(err)
			}
			consumer, err := rt.Instantiate(context.Background(), consumerModule, WithImports(NewImports().Table("provider", "objects", table)), WithGC(gcConfig))
			consumerModule.Close()
			if err != nil {
				provider.Close()
				t.Fatal(err)
			}
			if provider.gc == nil || provider.gc != consumer.gc || provider.refStore == nil || provider.refStore != consumer.refStore {
				consumer.Close()
				provider.Close()
				t.Fatal("shared rooted table did not unify collector ownership")
			}
			if got, callErr := provider.Invoke("set", 0, 55); callErr != nil || !slices.Equal(got, []uint64{55}) {
				t.Fatalf("provider set=%v err=%v", got, callErr)
			}
			if got, callErr := consumer.Invoke("read", 0); callErr != nil || !slices.Equal(got, []uint64{55}) {
				t.Fatalf("consumer read=%v err=%v", got, callErr)
			}
			if got, callErr := consumer.Invoke("grow", 1); callErr != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatalf("consumer grow=%v err=%v", got, callErr)
			}
			if got, callErr := consumer.Invoke("set", 1, 77); callErr != nil || !slices.Equal(got, []uint64{77}) {
				t.Fatalf("consumer set grown slot=%v err=%v", got, callErr)
			}
			if err := consumer.CollectGC(); err != nil {
				t.Fatal(err)
			}
			if err := provider.Close(); err != nil {
				t.Fatal(err)
			}
			if err := consumer.CollectGC(); err != nil {
				t.Fatal(err)
			}
			if got, callErr := consumer.Invoke("read", 1); callErr != nil || !slices.Equal(got, []uint64{77}) {
				t.Fatalf("consumer read after producer close=%v err=%v", got, callErr)
			}
			if err := consumer.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func rootedScalarAggregateTableBulkModule() []byte {
	get := func(index byte) []byte {
		return []byte{0x41, index, 0x25, 0x00, 0xfb, 0x02, 0x00, 0x00}
	}
	run := []byte{
		0x00,
		0x41, 0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0x26, 0x00, // table[0] = struct(42)
		0x41, 0x01, 0x41, 0x00, 0x41, 0x01, 0xfc, 0x0e, 0x00, 0x00, // copy table[0] to table[1]
		0x41, 0x00, 0xd0, 0x00, 0x26, 0x00, // clear the original root
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x41, 0x01, 0xfc, 0x0f, 0x00, 0x1a, // grow with struct(7)
		0x41, 0x00, 0x41, 0x09, 0xfb, 0x00, 0x00, 0x41, 0x01, 0xfc, 0x11, 0x00, // fill table[0] with struct(9)
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a, // force another collection after publication
	}
	run = append(run, get(0)...)
	run = append(run, get(1)...)
	run = append(run, get(2)...)
	run = append(run,
		0xfc, 0x10, 0x00, // table.size
		0xd0, 0x00, 0x41, 0x02, 0xfc, 0x0f, 0x00, // failed grow returns -1
		0x0b,
	)
	grow := []byte{0x00, 0xd0, 0x00, 0x20, 0x00, 0xfc, 0x0f, 0x00, 0x0b}
	read := []byte{0x00}
	read = append(read, get(0)...)
	read = append(read, get(1)...)
	read = append(read, get(2)...)
	read = append(read, 0x0b)
	fillNull := []byte{0x00, 0x20, 0x00, 0xd0, 0x00, 0x20, 0x01, 0xfc, 0x11, 0x00, 0x0b}
	copyRange := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0e, 0x00, 0x00, 0x0b}
	initRange := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x20, 0x02, 0xfc, 0x0c, 0x00, 0x00, 0x0b}
	dropElem := []byte{0x00, 0xfc, 0x0d, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, nil),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(3), wasmtest.ULEB(4), wasmtest.ULEB(5), wasmtest.ULEB(5), wasmtest.ULEB(6))),
		wasmtest.Section(4, []byte{0x01, 0x63, 0x00, 0x01, 0x02, 0x04}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("grow", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("read", byte(wasm.ExternFunc), 2),
			wasmtest.ExportEntry("fill-null", byte(wasm.ExternFunc), 3),
			wasmtest.ExportEntry("copy", byte(wasm.ExternFunc), 4),
			wasmtest.ExportEntry("init", byte(wasm.ExternFunc), 5),
			wasmtest.ExportEntry("drop", byte(wasm.ExternFunc), 6),
		)),
		wasmtest.Section(9, []byte{0x01, 0x05, 0x63, 0x00, 0x02, 0x41, 0x37, 0xfb, 0x00, 0x00, 0x0b, 0xd0, 0x00, 0x0b}),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(run))), run...),
			append(wasmtest.ULEB(uint32(len(grow))), grow...),
			append(wasmtest.ULEB(uint32(len(read))), read...),
			append(wasmtest.ULEB(uint32(len(fillNull))), fillNull...),
			append(wasmtest.ULEB(uint32(len(copyRange))), copyRange...),
			append(wasmtest.ULEB(uint32(len(initRange))), initRange...),
			append(wasmtest.ULEB(uint32(len(dropElem))), dropElem...),
		)),
	)
}

func rootedScalarAggregateActiveElementModule() []byte {
	body := []byte{
		0x00,
		0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a,
		0x41, 0x00, 0x25, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, []byte{0x01, 0x63, 0x00, 0x01, 0x01, 0x04}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("read", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(9, []byte{0x01, 0x06, 0x00, 0x41, 0x00, 0x0b, 0x63, 0x00, 0x01,
			0x41, 0x37, 0xfb, 0x00, 0x00, 0x0b}),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rootedScalarAggregateTableInitializerModule() []byte {
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
			0x01, 0x40, 0x00, 0x63, 0x00, 0x01, 0x03, 0x03,
			0x41, 0x37, 0xfb, 0x00, 0x00, 0x0b,
		}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rootedScalarAggregateSubtypeTableInitializerModule() []byte {
	body := []byte{0x00, 0x41, 0x01, 0xfb, 0x00, 0x01, 0x1a, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x01},       // open struct supertype
			[]byte{0x4f, 0x01, 0x00, 0x5f, 0x01, 0x7f, 0x01}, // final struct subtype
			[]byte{0x5f, 0x01, 0x7e, 0x01},                   // unrelated aggregate
			[]byte{0x5f, 0x01, 0x63, 0x00, 0x01},             // holder of (ref null 0)
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(4))),
		wasmtest.Section(4, []byte{
			0x01, 0x40, 0x00, 0x63, 0x03, 0x01, 0x02, 0x02,
			0x41, 0x37, 0xfb, 0x00, 0x01, 0xfb, 0x00, 0x03, 0x0b,
		}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rootedScalarAggregateGlobalInitializerModule() []byte {
	body := []byte{0x00, 0x41, 0x01, 0xfb, 0x00, 0x00, 0x1a, 0x41, 0x07, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(
			[]byte{
				0x64, 0x00, 0x00,
				0x41, 0x21, 0xfb, 0x00, 0x00, 0x0b,
			},
			[]byte{
				0x63, 0x00, 0x00,
				0x23, 0x00, 0x0b,
			},
		)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedAggregateGlobalInitializerExecutes(t *testing.T) {
	source := rootedScalarAggregateGlobalInitializerModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("aggregate global initializer module was not source-admitted")
	}
	decoded, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic string
	if plan := newGCFrameRootPlan(decoded, true, true, &diagnostic, nil); plan == nil || diagnostic != "" {
		t.Fatalf("aggregate global initializer exact-root plan=%+v diagnostic=%q", plan, diagnostic)
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Globals) != 2 || len(view.Globals[0].InitExpr) == 0 ||
		!view.Globals[1].HasInitGlobal || view.Globals[1].InitGlobal != 0 || len(view.Globals[1].InitExpr) != 0 {
		t.Fatalf("aggregate global initializer receipt=%t globals=%#v validation=%v", rv64FreshRootedGC(view), view.Globals, railshotValidateCompiled(view))
	}
	for name, mutate := range map[string]func(*Compiled){
		"missing": func(candidate *Compiled) { candidate.Globals[0].InitExpr = nil },
		"self":    func(candidate *Compiled) { candidate.Globals[0].InitExpr = []byte{0x23, 0x00, 0x0b} },
		"copy-self": func(candidate *Compiled) {
			candidate.Globals[1].InitGlobal = 1
		},
		"copy-mutable-source": func(candidate *Compiled) {
			candidate.Globals[0].Mutable = true
		},
		"copy-feature": func(candidate *Compiled) {
			candidate.requiredFeatures &^= CoreFeatureExtendedConstExpressions
		},
	} {
		t.Run("receipt-"+name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			candidate.Globals = slices.Clone(view.Globals)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated aggregate global initializer retained generic receipt")
			}
		})
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err = instance.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	if err = instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	ref := native.Ref(uint32(readGlobalObject(instance.globalCells[0], instance.c.Globals[0].Type)))
	copyRef := native.Ref(uint32(readGlobalObject(instance.globalCells[1], instance.c.Globals[1].Type)))
	if copyRef != ref {
		t.Fatalf("aggregate global copy=%v want=%v", copyRef, ref)
	}
	value, err := instance.gc.StructGet(ref, 0)
	if err != nil || value.Bits != 33 {
		t.Fatalf("aggregate global initializer = %v/%+v, %v", ref, value, err)
	}
}

func rootedScalarSelfRecursiveStructModule() []byte {
	body := []byte{
		0x00,
		0x23, 0x00, 0x41, 0x29, 0xfb, 0x00, 0x00, 0x24, 0x00,
		0x23, 0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0x24, 0x00,
		0xd0, 0x00, 0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
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

func TestRV64RootedSelfRecursiveStructExecutes(t *testing.T) {
	source := rootedScalarSelfRecursiveStructModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("self-recursive struct module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Types) < 1 || view.Types[0].RecGroup != 0 ||
		len(view.Types[0].Fields) != 2 || view.Types[0].Fields[0].Storage.Value.Ref.Heap.TypeIndex != 0 {
		t.Fatalf("self-recursive receipt=%t types=%+v validation=%v", rv64FreshRootedGC(view), view.Types, railshotValidateCompiled(view))
	}
	for name, mutate := range map[string]func(*Compiled){
		"rec-group": func(candidate *Compiled) {
			candidate.Types[0].RecGroup = 1
		},
		"field-target": func(candidate *Compiled) {
			candidate.Types[0].Fields = slices.Clone(candidate.Types[0].Fields)
			candidate.Types[0].Fields[0].Storage.Value.Ref.Heap.TypeIndex = 1
		},
	} {
		t.Run("receipt-"+name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			candidate.Types = slices.Clone(view.Types)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated self-recursive type retained generic receipt")
			}
		})
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err = instance.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	if err = instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	root := native.Ref(uint32(readGlobalObject(instance.globalCells[0], instance.c.Globals[0].Type)))
	childValue, err := instance.gc.StructGet(root, 0)
	if err != nil || childValue.Ref == 0 {
		t.Fatalf("self-recursive root child=%+v, %v", childValue, err)
	}
	rootValue, rootErr := instance.gc.StructGet(root, 1)
	childField, childErr := instance.gc.StructGet(childValue.Ref, 1)
	if rootErr != nil || childErr != nil || rootValue.Bits != 42 || childField.Bits != 41 {
		t.Fatalf("self-recursive values root=%+v/%v child=%+v/%v", rootValue, rootErr, childField, childErr)
	}
}

func rootedScalarMutuallyRecursiveStructModule() []byte {
	body := []byte{
		0x00,
		0xd0, 0x00, 0xfb, 0x00, 0x01, 0x24, 0x01,
		0x23, 0x01, 0xfb, 0x00, 0x00, 0x24, 0x00,
		0x23, 0x01, 0x23, 0x00, 0xfb, 0x05, 0x01, 0x00,
		0x23, 0x00, 0xfb, 0x02, 0x00, 0x00, 0xfb, 0x02, 0x01, 0x00, 0xd1,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{
				0x4e, 0x02,
				0x5f, 0x01, 0x63, 0x01, 0x01,
				0x5f, 0x01, 0x63, 0x00, 0x01,
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

func TestRV64RootedMutuallyRecursiveStructsExecute(t *testing.T) {
	source := rootedScalarMutuallyRecursiveStructModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("mutually recursive struct module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Types) < 3 || view.Types[0].RecGroup != 0 || view.Types[1].RecGroup != 0 || view.Types[2].RecGroup != 1 ||
		len(view.Types[0].Fields) != 1 || len(view.Types[1].Fields) != 1 ||
		view.Types[0].Fields[0].Storage.Value.Ref.Heap.TypeIndex != 1 || view.Types[1].Fields[0].Storage.Value.Ref.Heap.TypeIndex != 0 {
		t.Fatalf("mutually recursive receipt=%t types=%+v validation=%v", rv64FreshRootedGC(view), view.Types, railshotValidateCompiled(view))
	}
	for name, mutate := range map[string]func(*Compiled){
		"split-group": func(candidate *Compiled) {
			candidate.Types[1].RecGroup = 1
		},
		"field-target": func(candidate *Compiled) {
			candidate.Types[0].Fields = slices.Clone(candidate.Types[0].Fields)
			candidate.Types[0].Fields[0].Storage.Value.Ref.Heap.TypeIndex = 0
		},
		"recursive-exact": func(candidate *Compiled) {
			candidate.Types[0].Fields = slices.Clone(candidate.Types[0].Fields)
			candidate.Types[0].Fields[0].Storage.Value.Ref.Exact = true
		},
	} {
		t.Run("receipt-"+name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			candidate.Types = slices.Clone(view.Types)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated recursive group retained generic receipt")
			}
		})
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{0}) {
		t.Fatalf("run()=%v err=%v want=[0]", got, err)
	}
	if err = instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	a := native.Ref(uint32(readGlobalObject(instance.globalCells[0], instance.c.Globals[0].Type)))
	b, err := instance.gc.StructGet(a, 0)
	if err != nil || b.Ref == 0 {
		t.Fatalf("A.b=%+v, %v", b, err)
	}
	back, err := instance.gc.StructGet(b.Ref, 0)
	if err != nil || back.Ref != a {
		t.Fatalf("A.b.a=%+v, %v want=%v", back, err, a)
	}
}

func rootedScalarMutuallyRecursiveStructArrayModule() []byte {
	body := []byte{
		0x00,
		0xd0, 0x00, 0x41, 0x01, 0xfb, 0x06, 0x01, 0x24, 0x01,
		0x23, 0x01, 0xfb, 0x00, 0x00, 0x24, 0x00,
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

func TestRV64RootedMutuallyRecursiveStructArrayExecutes(t *testing.T) {
	source := rootedScalarMutuallyRecursiveStructArrayModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("mutually recursive struct/array module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Types) < 3 || view.Types[0].RecGroup != 0 || view.Types[1].RecGroup != 0 ||
		view.Types[0].Kind != CompositeTypeStruct || view.Types[1].Kind != CompositeTypeArray ||
		view.Types[0].Fields[0].Storage.Value.Ref.Heap.TypeIndex != 1 || view.Types[1].Array.Storage.Value.Ref.Heap.TypeIndex != 0 {
		t.Fatalf("recursive struct/array receipt=%t types=%+v validation=%v", rv64FreshRootedGC(view), view.Types, railshotValidateCompiled(view))
	}
	candidate := cloneCompiledMetadata(view)
	candidate.Types = slices.Clone(view.Types)
	candidate.Types[1].Array.Storage.Value.Ref.Exact = true
	if rv64FreshRootedGC(candidate) {
		t.Fatal("exact recursive array element retained generic receipt")
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{0}) {
		t.Fatalf("run()=%v err=%v want=[0]", got, err)
	}
	if err = instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	a := native.Ref(uint32(readGlobalObject(instance.globalCells[0], instance.c.Globals[0].Type)))
	b, err := instance.gc.StructGet(a, 0)
	if err != nil || b.Ref == 0 {
		t.Fatalf("A.b=%+v, %v", b, err)
	}
	back, err := instance.gc.ArrayGet(b.Ref, 0)
	if err != nil || back.Ref != a {
		t.Fatalf("A.b[0]=%+v, %v want=%v", back, err, a)
	}
}

func rootedScalarSelfRecursiveArrayModule() []byte {
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

func TestRV64RootedSelfRecursiveArrayExecutes(t *testing.T) {
	source := rootedScalarSelfRecursiveArrayModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("self-recursive array module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Types) < 1 || view.Types[0].RecGroup != 0 ||
		view.Types[0].Array.Storage.Value.Ref.Heap.TypeIndex != 0 {
		t.Fatalf("self-recursive array receipt=%t types=%+v validation=%v", rv64FreshRootedGC(view), view.Types, railshotValidateCompiled(view))
	}
	for name, mutate := range map[string]func(*Compiled){
		"rec-group": func(candidate *Compiled) {
			candidate.Types[0].RecGroup = 1
		},
		"element-target": func(candidate *Compiled) {
			candidate.Types[0].Array.Storage.Value.Ref.Heap.TypeIndex = 1
		},
	} {
		t.Run("receipt-"+name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			candidate.Types = slices.Clone(view.Types)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated self-recursive array retained generic receipt")
			}
		})
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err = instance.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	if err = instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	root := native.Ref(uint32(readGlobalObject(instance.globalCells[0], instance.c.Globals[0].Type)))
	child, err := instance.gc.ArrayGet(root, 0)
	if err != nil || child.Ref == 0 {
		t.Fatalf("self-recursive array child=%+v, %v", child, err)
	}
	leaf, err := instance.gc.ArrayGet(child.Ref, 0)
	if err != nil || leaf.Ref != 0 {
		t.Fatalf("self-recursive array leaf=%+v, %v", leaf, err)
	}
}

func rootedScalarExactAggregateStorageModule(array bool) []byte {
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

func rootedScalarExactAggregateSubtypeCastModule() []byte {
	body := []byte{0x00,
		0x41, 0x29, 0xfb, 0x00, 0x01,
		0xfb, 0x16, 0x62, 0x00,
		0xfb, 0x00, 0x02, 0x1a,
		0x41, 0x01, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x01},
			[]byte{0x4f, 0x01, 0x00, 0x5f, 0x01, 0x7f, 0x01},
			[]byte{0x5f, 0x01, 0x64, 0x62, 0x00, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(3))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedExactAggregateStorageExecutes(t *testing.T) {
	for _, array := range []bool{false, true} {
		t.Run(fmt.Sprintf("array=%t", array), func(t *testing.T) {
			source := rootedScalarExactAggregateStorageModule(array)
			if !railshot.AdmitGCRootedScalarSource(source) {
				t.Fatal("exact aggregate field module was not source-admitted")
			}
			compiled, err := Compile(rv64RootedScalarConfig(), source)
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			view := compiled.executionView()
			storage := view.Types[1].Array.Storage
			if !array {
				storage = view.Types[1].Fields[0].Storage
			}
			if !rv64FreshRootedGC(view) || !storage.Value.Ref.Exact || storage.Value.Ref.Heap.TypeIndex != 0 {
				t.Fatalf("exact aggregate field receipt=%t types=%+v validation=%v", rv64FreshRootedGC(view), view.Types, railshotValidateCompiled(view))
			}
			mutated := cloneCompiledMetadata(view)
			mutated.Types = slices.Clone(view.Types)
			if array {
				mutated.Types[1].Array.Storage.Value.Ref.Heap.TypeIndex = 2
			} else {
				mutated.Types[1].Fields = slices.Clone(view.Types[1].Fields)
				mutated.Types[1].Fields[0].Storage.Value.Ref.Heap.TypeIndex = 2
			}
			if rv64FreshRootedGC(mutated) {
				t.Fatal("out-of-scope exact aggregate field retained generic receipt")
			}
			mutated = cloneCompiledMetadata(view)
			mutated.Types = slices.Clone(view.Types)
			if array {
				mutated.Types[1].Array.Storage.Value.Ref.Exact = false
			} else {
				mutated.Types[1].Fields = slices.Clone(view.Types[1].Fields)
				mutated.Types[1].Fields[0].Storage.Value.Ref.Exact = false
			}
			if rv64FreshRootedGC(mutated) {
				t.Fatal("mutated exact aggregate contract retained generic receipt")
			}
			instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{41}) {
				t.Fatalf("run()=%v err=%v want=[41]", got, err)
			}
			if err = instance.CollectGC(); err != nil {
				t.Fatal(err)
			}
			outer := native.Ref(uint32(readGlobalObject(instance.globalCells[0], instance.c.Globals[0].Type)))
			child, err := instance.gc.StructGet(outer, 0)
			if array {
				child, err = instance.gc.ArrayGet(outer, 0)
			}
			if err != nil || child.Ref == 0 {
				t.Fatalf("exact aggregate child=%+v, %v", child, err)
			}
			value, err := instance.gc.StructGet(child.Ref, 0)
			if err != nil || value.Bits != 41 {
				t.Fatalf("exact aggregate value=%+v, %v", value, err)
			}
		})
	}
}

func TestRV64RootedExactAggregateSubtypeCastTraps(t *testing.T) {
	source := rootedScalarExactAggregateSubtypeCastModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("exact aggregate subtype-cast module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if view := compiled.executionView(); !rv64FreshRootedGC(view) {
		t.Fatalf("exact subtype-cast receipt=false validation=%v", railshotValidateCompiled(view))
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err = instance.Invoke("run"); err == nil {
		t.Fatal("exact cast accepted a proper subtype")
	} else if trap, ok := err.(*TrapError); !ok || trap.Code != TrapCastFailure {
		t.Fatalf("exact subtype cast trap=%v, want %v", err, TrapCastFailure)
	}
}

func TestRV64RootedAggregateTableInitializerExecutes(t *testing.T) {
	source := rootedScalarAggregateTableInitializerModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("aggregate table initializer module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Elems) != 1 || len(view.Elems[0].Values) != 3 || len(view.Elems[0].Values[0].Expr) == 0 ||
		!view.Elems[0].Values[1].RepeatPrevious || !view.Elems[0].Values[2].RepeatPrevious {
		t.Fatalf("aggregate table initializer receipt=%t elems=%#v validation=%v", rv64FreshRootedGC(view), view.Elems, railshotValidateCompiled(view))
	}
	for name, mutate := range map[string]func(*Compiled){
		"expression": func(candidate *Compiled) {
			candidate.Elems = slices.Clone(candidate.Elems)
			candidate.Elems[0].Values = slices.Clone(candidate.Elems[0].Values)
			candidate.Elems[0].Values[0].Expr = []byte{0xfb, 0x01, 0x00, 0x0b}
		},
		"repeat": func(candidate *Compiled) {
			candidate.Elems = slices.Clone(candidate.Elems)
			candidate.Elems[0].Values = slices.Clone(candidate.Elems[0].Values)
			candidate.Elems[0].Values[1].RepeatPrevious = false
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated table initializer retained the generic receipt")
			}
		})
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	if err := instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	descriptor := instance.tableDescriptor(0)
	want := native.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8:])))
	if want.IsNull() {
		t.Fatal("aggregate table initializer produced null")
	}
	for slot := 1; slot < 3; slot++ {
		got := native.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8+slot*8:])))
		if got != want {
			t.Fatalf("aggregate table initializer slot %d = %v, want shared %v", slot, got, want)
		}
	}
	value, err := instance.gc.StructGet(want, 0)
	if err != nil || value.Bits != 55 {
		t.Fatalf("aggregate table initializer field = %+v, %v", value, err)
	}
}

func TestRV64RootedAggregateSubtypeTableInitializerExecutes(t *testing.T) {
	source := rootedScalarAggregateSubtypeTableInitializerModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("aggregate subtype table initializer module was not source-admitted")
	}
	decoded, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	var rootDiagnostic string
	if plan := newGCFrameRootPlan(decoded, true, true, &rootDiagnostic, nil); plan == nil || rootDiagnostic != "" {
		t.Fatalf("aggregate subtype table initializer exact-root plan=%+v diagnostic=%q", plan, rootDiagnostic)
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Elems) != 1 || len(view.Elems[0].Values) != 2 || !view.Elems[0].Values[1].RepeatPrevious {
		t.Fatalf("aggregate subtype table initializer receipt=%t elems=%#v validation=%v", rv64FreshRootedGC(view), view.Elems, railshotValidateCompiled(view))
	}
	for name, mutate := range map[string]func(*Compiled){
		"type-super": func(candidate *Compiled) {
			candidate.Types[1].Supers = nil
		},
		"descriptor-super": func(candidate *Compiled) {
			candidate.GCTypeDescs[1].HasSuper = false
		},
		"final-super": func(candidate *Compiled) {
			candidate.Types[0].Final = true
			candidate.GCTypeDescs[0].Final = true
		},
		"incompatible-layout": func(candidate *Compiled) {
			candidate.Types[1].Fields[0].Storage.Value = ValueTypeDescriptor{Kind: ValueTypeI64}
			desc, err := native.NewStructDesc(1, []native.StorageKind{native.StorageI64})
			if err != nil {
				t.Fatal(err)
			}
			desc.Final, desc.Super, desc.HasSuper = true, 0, true
			candidate.GCTypeDescs[1] = desc
		},
	} {
		t.Run("receipt-"+name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated aggregate subtype metadata retained the generic receipt")
			}
		})
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	if err := instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	descriptor := instance.tableDescriptor(0)
	holder := native.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8:])))
	repeated := native.Ref(uint32(binary.LittleEndian.Uint64(descriptor[16:])))
	if holder.IsNull() || repeated != holder {
		t.Fatalf("aggregate subtype table initializer refs = %v/%v", holder, repeated)
	}
	childValue, err := instance.gc.StructGet(holder, 0)
	if err != nil {
		t.Fatal(err)
	}
	value, err := instance.gc.StructGet(childValue.Ref, 0)
	if err != nil || value.Bits != 55 {
		t.Fatalf("aggregate subtype child = %v/%+v, %v", childValue.Ref, value, err)
	}
}

func rootedScalarAggregateGlobalTableInitializerModule() []byte {
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

func rootedScalarAggregateImportedGlobalTableInitializerModule() []byte {
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

func rootedScalarAggregateReferenceGlobalTableInitializerModule() []byte {
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

func TestRV64RootedAggregateGlobalTableInitializerExecutes(t *testing.T) {
	source := rootedScalarAggregateGlobalTableInitializerModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("aggregate global table initializer module was not source-admitted")
	}
	decoded, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	var rootDiagnostic string
	if plan := newGCFrameRootPlan(decoded, true, true, &rootDiagnostic, nil); plan == nil || rootDiagnostic != "" {
		t.Fatalf("aggregate global table initializer exact-root plan=%+v diagnostic=%q", plan, rootDiagnostic)
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Globals) != 1 || len(view.Elems) != 1 || len(view.Elems[0].Values) != 2 ||
		len(view.Elems[0].Values[0].Expr) == 0 || !view.Elems[0].Values[1].RepeatPrevious {
		t.Fatalf("aggregate global table initializer receipt=%t globals=%#v elems=%#v validation=%v", rv64FreshRootedGC(view), view.Globals, view.Elems, railshotValidateCompiled(view))
	}
	if !view.requiredFeatures.IsEnabled(CoreFeatureExtendedConstExpressions) || !compiledStructuralRequiredFeatures(view).IsEnabled(CoreFeatureExtendedConstExpressions) {
		t.Fatalf("aggregate local-global initializer feature receipt=%#x structural=%#x", view.requiredFeatures, compiledStructuralRequiredFeatures(view))
	}
	candidate := cloneCompiledMetadata(view)
	candidate.requiredFeatures &^= CoreFeatureExtendedConstExpressions
	if rv64FreshRootedGC(candidate) {
		t.Fatal("aggregate local-global initializer retained the generic receipt without Extended Const Expressions")
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	if err := instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	descriptor := instance.tableDescriptor(0)
	want := native.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8:])))
	got := native.Ref(uint32(binary.LittleEndian.Uint64(descriptor[16:])))
	if want.IsNull() || got != want {
		t.Fatalf("aggregate global table initializer refs = %v/%v", want, got)
	}
	value, err := instance.gc.StructGet(want, 0)
	if err != nil || value.Bits != 55 {
		t.Fatalf("aggregate global table initializer field = %+v, %v", value, err)
	}
}

func TestRV64RootedAggregateImportedGlobalTableInitializerExecutes(t *testing.T) {
	source := rootedScalarAggregateImportedGlobalTableInitializerModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("aggregate imported-global table initializer module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.GlobalImports) != 1 || len(view.Elems) != 1 || len(view.Elems[0].Values) != 2 ||
		len(view.Elems[0].Values[0].Expr) == 0 || !view.Elems[0].Values[1].RepeatPrevious {
		t.Fatalf("aggregate imported-global table initializer receipt=%t imports=%#v elems=%#v validation=%v", rv64FreshRootedGC(view), view.GlobalImports, view.Elems, railshotValidateCompiled(view))
	}
	if view.requiredFeatures.IsEnabled(CoreFeatureExtendedConstExpressions) || compiledStructuralRequiredFeatures(view).IsEnabled(CoreFeatureExtendedConstExpressions) {
		t.Fatalf("aggregate imported-global initializer unexpectedly requires Extended Const Expressions: receipt=%#x structural=%#x", view.requiredFeatures, compiledStructuralRequiredFeatures(view))
	}
	instance, err := Instantiate(compiled, InstantiateOptions{
		Imports: NewImports().Global("env", "base", GlobalImport{Type: ValI32, Bits: 55}),
		GC:      GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	if err := instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	descriptor := instance.tableDescriptor(0)
	want := native.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8:])))
	got := native.Ref(uint32(binary.LittleEndian.Uint64(descriptor[16:])))
	if want.IsNull() || got != want {
		t.Fatalf("aggregate imported-global table initializer refs = %v/%v", want, got)
	}
	value, err := instance.gc.StructGet(want, 0)
	if err != nil || value.Bits != 55 {
		t.Fatalf("aggregate imported-global table initializer field = %+v, %v", value, err)
	}
}

func TestRV64RootedAggregateReferenceGlobalTableInitializerExecutes(t *testing.T) {
	source := rootedScalarAggregateReferenceGlobalTableInitializerModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("aggregate reference-global table initializer module was not source-admitted")
	}
	decoded, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	var rootDiagnostic string
	if plan := newGCFrameRootPlan(decoded, true, true, &rootDiagnostic, nil); plan == nil || rootDiagnostic != "" {
		t.Fatalf("aggregate reference-global table initializer exact-root plan=%+v diagnostic=%q", plan, rootDiagnostic)
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Globals) != 1 || len(view.Elems) != 1 || len(view.Elems[0].Values) != 2 ||
		len(view.Elems[0].Values[0].Expr) == 0 || !view.Elems[0].Values[1].RepeatPrevious {
		t.Fatalf("aggregate reference-global table initializer receipt=%t globals=%#v elems=%#v validation=%v", rv64FreshRootedGC(view), view.Globals, view.Elems, railshotValidateCompiled(view))
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	if err := instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	descriptor := instance.tableDescriptor(0)
	want := native.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8:])))
	got := native.Ref(uint32(binary.LittleEndian.Uint64(descriptor[16:])))
	if want.IsNull() || got != want {
		t.Fatalf("aggregate reference-global table initializer refs = %v/%v", want, got)
	}
	value, err := instance.gc.StructGet(want, 0)
	if err != nil || value.Bits != 0 {
		t.Fatalf("aggregate reference-global table initializer field = %+v, %v", value, err)
	}
}

func rootedScalarAggregateFunctionReferenceElementModule() []byte {
	body := []byte{
		0x00,
		0x41, 0x01, 0xfb, 0x00, 0x01, 0x1a, // numeric struct allocation/safepoint
		0x0b,
	}
	return wasmtest.Module(
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
}

func rootedScalarAggregateArrayActiveElementModule() []byte {
	body := []byte{
		0x00,
		0x41, 0x01, 0x41, 0x02, 0xfb, 0x08, 0x00, 0x02, 0x1a,
		0x0b,
	}
	return wasmtest.Module(
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
}

func TestRV64RootedAggregateArrayActiveElementExecutes(t *testing.T) {
	source := rootedScalarAggregateArrayActiveElementModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("aggregate array active element module was not source-admitted")
	}
	decoded, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	var rootDiagnostic string
	if plan := newGCFrameRootPlan(decoded, true, true, &rootDiagnostic, nil); plan == nil || rootDiagnostic != "" {
		t.Fatalf("aggregate array active element exact-root plan=%+v diagnostic=%q", plan, rootDiagnostic)
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Elems) != 1 || len(view.Elems[0].Values) != 1 || len(view.Elems[0].Values[0].Expr) == 0 {
		t.Fatalf("aggregate array active element receipt=%t elems=%#v validation=%v", rv64FreshRootedGC(view), view.Elems, railshotValidateCompiled(view))
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	if err := instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	descriptor := instance.tableDescriptor(0)
	ref := native.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8:])))
	for index, want := range []uint64{11, 22} {
		value, err := instance.gc.ArrayGet(ref, uint32(index))
		if err != nil || value.Bits != want {
			t.Fatalf("active array root[%d] = %+v, %v; want %d", index, value, err, want)
		}
	}
}

func TestRV64RootedAggregateActiveElementExecutes(t *testing.T) {
	source := rootedScalarAggregateActiveElementModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("aggregate active element module was not source-admitted")
	}
	decoded, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	var rootDiagnostic string
	if plan := newGCFrameRootPlan(decoded, true, true, &rootDiagnostic, nil); plan == nil || rootDiagnostic != "" {
		t.Fatalf("aggregate active element exact-root plan=%+v diagnostic=%q", plan, rootDiagnostic)
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Elems) != 1 || len(view.Elems[0].Values) != 1 || len(view.Elems[0].Values[0].Expr) == 0 {
		t.Fatalf("aggregate active element receipt=%t elems=%#v validation=%v", rv64FreshRootedGC(view), view.Elems, railshotValidateCompiled(view))
	}
	mutated := cloneCompiledMetadata(view)
	mutated.Elems = slices.Clone(view.Elems)
	mutated.Elems[0].Values = slices.Clone(view.Elems[0].Values)
	mutated.Elems[0].Values[0].Expr = slices.Clone(view.Elems[0].Values[0].Expr)
	mutated.Elems[0].Values[0].Expr[1] = 0x38
	if rv64FreshRootedGC(mutated) {
		t.Fatal("mutated aggregate active element retained the rooted receipt")
	}
	for iteration := range 8 {
		instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatalf("iteration %d: %v", iteration, err)
		}
		if err := instance.CollectGC(); err != nil {
			instance.Close()
			t.Fatalf("iteration %d: collect active root: %v", iteration, err)
		}
		got, err := instance.Invoke("read")
		instance.Close()
		if err != nil || !slices.Equal(got, []uint64{55}) {
			t.Fatalf("iteration %d: read()=%v err=%v", iteration, got, err)
		}
	}
}

func TestRV64RootedAggregateFunctionReferenceElementExecutes(t *testing.T) {
	source := rootedScalarAggregateFunctionReferenceElementModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("aggregate function-reference element module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Types) != 3 || len(view.Types[0].Fields) != 1 ||
		view.Types[0].Fields[0].Storage.Value.Ref.Heap.Abstract != AbstractHeapFunc {
		t.Fatalf("aggregate function-reference receipt=%t types=%#v validation=%v", rv64FreshRootedGC(view), view.Types, railshotValidateCompiled(view))
	}
	mutated := cloneCompiledMetadata(view)
	mutated.Types = slices.Clone(view.Types)
	mutated.Types[0].Fields = slices.Clone(view.Types[0].Fields)
	mutated.Types[0].Fields[0].Storage.Value.Ref.Heap.Abstract = AbstractHeapExtern
	if rv64FreshRootedGC(mutated) {
		t.Fatal("mutated aggregate function-reference storage retained the rooted receipt")
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	if err := instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	descriptor := instance.tableDescriptor(0)
	outer := native.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8:])))
	value, err := instance.gc.StructGet(outer, 0)
	if err != nil || value.Kind != native.StorageFuncRef || value.Bits == 0 {
		t.Fatalf("active aggregate function reference = %+v, %v", value, err)
	}
}

func TestRV64RootedAggregateTableBulkMutationExecutes(t *testing.T) {
	source := rootedScalarAggregateTableBulkModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("aggregate table bulk module was not source-admitted")
	}
	decoded, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	var rootDiagnostic string
	if plan := newGCFrameRootPlan(decoded, true, true, &rootDiagnostic, nil); plan == nil || rootDiagnostic != "" {
		t.Fatalf("aggregate table bulk exact-root plan=%+v diagnostic=%q", plan, rootDiagnostic)
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || view.syncHostSlots < 5 || len(view.passiveElems) != 1 || len(view.passiveElems[0].Values) != 2 || len(view.passiveElems[0].Values[0].Expr) == 0 || !view.passiveElems[0].Values[1].Null {
		t.Fatalf("aggregate-table bulk receipt=%t slots=%d validation=%v", rv64FreshRootedGC(view), view.syncHostSlots, railshotValidateCompiled(view))
	}
	mutated := cloneCompiledMetadata(view)
	mutated.syncHostSlots = 4
	if rv64FreshRootedGC(mutated) {
		t.Fatal("shortened bulk helper arena retained the rooted receipt")
	}
	mutated = cloneCompiledMetadata(view)
	mutated.passiveElems = slices.Clone(view.passiveElems)
	mutated.passiveElems[0].Values = slices.Clone(view.passiveElems[0].Values)
	mutated.passiveElems[0].Values[0].Expr = slices.Clone(mutated.passiveElems[0].Values[0].Expr)
	mutated.passiveElems[0].Values[0].Expr[1] = 0x38
	if rv64FreshRootedGC(mutated) {
		t.Fatal("mutated aggregate passive element retained the rooted receipt")
	}
	for iteration := range 8 {
		instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{9, 42, 7, 3, uint64(^uint32(0))}) {
			instance.Close()
			t.Fatalf("iteration %d: run()=%v err=%v", iteration, got, err)
		}
		if got, err := instance.Invoke("grow", 0xfeedface00000001); err != nil || !slices.Equal(got, []uint64{3}) {
			instance.Close()
			t.Fatalf("iteration %d: dirty-upper grow()=%v err=%v", iteration, got, err)
		}
		descriptor := instance.tableDescriptor(0)
		if got, err := instance.Invoke("fill-null", 0xfeedface00000003, 0xdeadbeef00000001); err != nil || len(got) != 0 {
			instance.Close()
			t.Fatalf("iteration %d: dirty-upper fill()=%v err=%v", iteration, got, err)
		}
		if got, err := instance.Invoke("copy", 0xfeedface00000004, 0xdeadbeef00000004, 0xbaadf00d00000000); err != nil || len(got) != 0 {
			instance.Close()
			t.Fatalf("iteration %d: dirty-upper zero-length copy()=%v err=%v", iteration, got, err)
		}
		beforeFailure := bytes.Clone(descriptor)
		got, err := instance.Invoke("fill-null", 0xfeedface00000004, 0xdeadbeef00000001)
		var trap *TrapError
		if got != nil || !errors.As(err, &trap) || trap.Code != TrapTableOutOfBounds {
			instance.Close()
			t.Fatalf("iteration %d: dirty-upper out-of-bounds fill()=%v err=%v trap=%+v", iteration, got, err, trap)
		}
		if !bytes.Equal(descriptor, beforeFailure) {
			instance.Close()
			t.Fatalf("iteration %d: failed fill changed descriptor %x -> %x", iteration, beforeFailure, descriptor)
		}
		if got, err := instance.Invoke("grow", 0xdeadbeef00000001); err != nil || !slices.Equal(got, []uint64{uint64(^uint32(0))}) {
			instance.Close()
			t.Fatalf("iteration %d: failed grow()=%v err=%v", iteration, got, err)
		}
		if !bytes.Equal(descriptor, beforeFailure) || binary.LittleEndian.Uint32(descriptor) != 4 || binary.LittleEndian.Uint64(descriptor[8+3*8:]) != 0 {
			instance.Close()
			t.Fatalf("iteration %d: failed grow changed descriptor %x -> %x", iteration, beforeFailure, descriptor)
		}
		if got, err := instance.Invoke("read"); err != nil || !slices.Equal(got, []uint64{9, 42, 7}) {
			instance.Close()
			t.Fatalf("iteration %d: read()=%v err=%v", iteration, got, err)
		}
		if got, err := instance.Invoke("init", 0xfeedface00000000, 0xdeadbeef00000000, 0xbaadf00d00000001); err != nil || len(got) != 0 || binary.LittleEndian.Uint64(descriptor[8:]) == 0 {
			instance.Close()
			t.Fatalf("iteration %d: dirty-upper init()=%v err=%v descriptor=%x", iteration, got, err, descriptor)
		}
		initialized := native.Ref(uint32(binary.LittleEndian.Uint64(descriptor[8:])))
		if got, err := instance.Invoke("drop"); err != nil || len(got) != 0 {
			instance.Close()
			t.Fatalf("iteration %d: drop()=%v err=%v", iteration, got, err)
		}
		if err := instance.CollectGC(); err != nil {
			instance.Close()
			t.Fatalf("iteration %d: collect after table.init/drop: %v", iteration, err)
		}
		if value, err := instance.gc.StructGet(initialized, 0); err != nil || value.Bits != 55 {
			instance.Close()
			t.Fatalf("iteration %d: initialized table root after drop/GC = %+v, %v", iteration, value, err)
		}
		if got, err := instance.Invoke("init", 0xfeedface00000004, 0xdeadbeef00000000, 0xbaadf00d00000000); err != nil || len(got) != 0 {
			instance.Close()
			t.Fatalf("iteration %d: zero init after drop()=%v err=%v", iteration, got, err)
		}
		got, err = instance.Invoke("init", 0xfeedface00000000, 0xdeadbeef00000000, 0xbaadf00d00000001)
		var elemTrap *TrapError
		if got != nil || !errors.As(err, &elemTrap) || elemTrap.Code != TrapTableOutOfBounds {
			instance.Close()
			t.Fatalf("iteration %d: nonzero init after drop()=%v err=%v trap=%+v", iteration, got, err, elemTrap)
		}
		instance.Close()
	}
}

func rootedScalarAggregateTableBoundsModule() []byte {
	set := []byte{
		0x00,
		0x20, 0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00,
		0x26, 0x00,
		0x0b,
	}
	get := []byte{
		0x00,
		0x41, 0x00, 0x25, 0x00,
		0xfb, 0x02, 0x00, 0x00,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(4, []byte{0x01, 0x63, 0x00, 0x01, 0x01, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("set", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("get", byte(wasm.ExternFunc), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(set))), set...),
			append(wasmtest.ULEB(uint32(len(get))), get...),
		)),
	)
}

func TestRV64RootedAggregateTableSetBoundsAndI32Normalization(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedScalarAggregateTableBoundsModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("set", 0xfeedface00000000); err != nil || len(got) != 0 {
		t.Fatalf("dirty-upper in-bounds set=%v err=%v", got, err)
	}
	got, err := instance.Invoke("set", 0xfeedface00000001)
	var trap *TrapError
	if got != nil || !errors.As(err, &trap) || trap.Code != TrapTableOutOfBounds {
		t.Fatalf("dirty-upper index==size set=%v err=%v trap=%+v, want %v", got, err, trap, TrapTableOutOfBounds)
	}
	if got, err := instance.Invoke("get"); err != nil || !slices.Equal(got, []uint64{42}) {
		t.Fatalf("table changed after failed set: get()=%v err=%v want=[42]", got, err)
	}
}

func rootedScalarIndirectTableModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	target := []byte{0x00, 0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a, 0x41, 0x2a, 0x0b}
	caller := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x29, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x41, 0x00, 0x11, 0x01, 0x00,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x6a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(4, []byte{0x01, 0x70, 0x01, 0x01, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(9, []byte{0x01, 0x00, 0x41, 0x00, 0x0b, 0x01, 0x00}),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(target))), target...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func TestRV64RootedScalarIndirectTableExecutes(t *testing.T) {
	source := rootedScalarIndirectTableModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("bounded rooted indirect-table module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if fresh, validation := rv64FreshRootedGC(view), railshotValidateCompiled(view); !fresh || validation != nil || view.tableCount() != 1 || view.TableSize != 1 || view.TableMax != 1 || len(view.Elems) != 1 || len(view.Elems[0].Values) != 1 || view.Elems[0].Values[0].FuncIndex != 0 {
		t.Fatalf("indirect-table generic rooted receipt is missing: fresh=%t validation=%v", fresh, validation)
	}
	for name, mutate := range map[string]func(*Compiled){
		"size":    func(candidate *Compiled) { candidate.TableSize = 2 },
		"maximum": func(candidate *Compiled) { candidate.TableMax = 2 },
		"target": func(candidate *Compiled) {
			candidate.Elems = slices.Clone(candidate.Elems)
			candidate.Elems[0].Values = slices.Clone(candidate.Elems[0].Values)
			candidate.Elems[0].Values[0].FuncIndex = 1
		},
		"offset": func(candidate *Compiled) {
			candidate.Elems = slices.Clone(candidate.Elems)
			candidate.Elems[0].Offset.Base = 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated indirect table retained the generic receipt")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatalf("mutated indirect table validation=%v", err)
			}
		})
	}
	for range 2 {
		instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{83}) {
			_ = instance.Close()
			t.Fatalf("run()=%v err=%v want=[83]", got, err)
		}
		if err := instance.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func rootedScalarImportedTableModule(exportTable bool) []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	tableImport := append(append(wasmtest.Name("env"), wasmtest.Name("table")...), byte(wasm.ExternTable), 0x70, 0x01, 0x01, 0x02)
	caller := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x29, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x41, 0x00, 0x11, 0x01, 0x00,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x6a,
		0x0b,
	}
	exports := [][]byte{wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0)}
	if exportTable {
		exports = append(exports, wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(caller))), caller...))),
	)
}

func rootedScalarTwoImportedTablesModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	tableImport := func(name string) []byte {
		return append(append(wasmtest.Name("env"), wasmtest.Name(name)...), byte(wasm.ExternTable), 0x70, 0x01, 0x01, 0x02)
	}
	caller := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x29, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x41, 0x00, 0x11, 0x01, 0x01,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x6a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(tableImport("first"), tableImport("second"))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(caller))), caller...))),
	)
}

func rootedScalarImportedTableProviderModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, []byte{0x01, 0x70, 0x01, 0x01, 0x02}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0))),
		wasmtest.Section(9, []byte{0x01, 0x00, 0x41, 0x00, 0x0b, 0x01, 0x00}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a, 0x41, 17, 0x0b}))),
	)
}

func rootedScalarPublicTableHostTargetModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	functionImport := append(append(wasmtest.Name("env"), wasmtest.Name("host")...), byte(wasm.ExternFunc))
	functionImport = append(functionImport, wasmtest.ULEB(1)...)
	caller := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x29, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x41, 0x00, 0x11, 0x01, 0x00,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x6a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(functionImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(4, []byte{0x01, 0x70, 0x01, 0x01, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("table", byte(wasm.ExternTable), 0),
		)),
		wasmtest.Section(9, []byte{0x01, 0x00, 0x41, 0x00, 0x0b, 0x01, 0x00}),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(caller))), caller...))),
	)
}

func TestRV64RootedScalarImportedTableExecutes(t *testing.T) {
	for _, exportTable := range []bool{false, true} {
		t.Run(fmt.Sprintf("export%t", exportTable), func(t *testing.T) {
			source := rootedScalarImportedTableModule(exportTable)
			if !railshot.AdmitGCRootedScalarSource(source) {
				t.Fatal("bounded rooted imported-table module was not source-admitted")
			}
			consumerCode, err := Compile(rv64RootedScalarConfig(), source)
			if err != nil {
				t.Fatal(err)
			}
			defer consumerCode.Close()
			view := consumerCode.executionView()
			roots := view.genericGCFrameRoots()
			if !rv64FreshRootedGC(view) || view.tableImportCount() != 1 || view.tableCount() != 1 || roots == nil || len(roots.callsites) != 1 || roots.callsites[0].stackAdjust == 0 || len(roots.callsites[0].offsets) != 1 {
				t.Fatalf("imported-table receipt=%t imports=%d tables=%d roots=%+v validation=%v", rv64FreshRootedGC(view), view.tableImportCount(), view.tableCount(), roots, railshotValidateCompiled(view))
			}
			mutated := cloneCompiledMetadata(view)
			mutated.tableImportMin++
			if rv64FreshRootedGC(mutated) {
				t.Fatal("mutated imported-table limit retained the generic receipt")
			}
			if exportTable {
				mutated = cloneCompiledMetadata(view)
				mutated.tableExports = map[string]int{"table": 1}
				if rv64FreshRootedGC(mutated) {
					t.Fatal("mutated imported-table export retained the generic receipt")
				}
			}
			providerCode, err := Compile(rv64RootedScalarConfig(), rootedScalarImportedTableProviderModule())
			if err != nil {
				t.Fatal(err)
			}
			defer providerCode.Close()
			rt := NewRuntime(WithRuntimeConfig(rv64RootedScalarConfig()))
			defer rt.Close()
			providerModule, err := rt.Module(providerCode)
			if err != nil {
				t.Fatal(err)
			}
			provider, err := rt.Instantiate(context.Background(), providerModule, WithGC(GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}))
			providerModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			defer provider.Close()
			table, err := provider.ExportedTable("table")
			if err != nil {
				t.Fatal(err)
			}
			defer table.Close()
			consumerModule, err := rt.Module(consumerCode)
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := rt.Instantiate(context.Background(), consumerModule,
				WithImports(NewImports().Table("env", "table", table)),
				WithGC(GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}),
			)
			consumerModule.Close()
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			if provider.gc == nil || provider.gc != consumer.gc {
				t.Fatalf("imported table did not join the provider and consumer GC domains: provider=%p consumer=%p", provider.gc, consumer.gc)
			}
			if provider.refStore == nil || provider.refStore != consumer.refStore {
				t.Fatalf("imported table did not retain one reference store: provider=%p consumer=%p", provider.refStore, consumer.refStore)
			}
			if !provider.refStore.ownsGCCollector(provider.gc) {
				t.Fatal("imported table GC domain collector is not owned by the shared reference store")
			}
			consumerReturnPC := consumer.base + uintptr(roots.callsites[0].returnOffset)
			if owner := provider.refStore.gcFrameOwner(consumerReturnPC, provider.gc); owner != consumer {
				t.Fatalf("consumer table-bridge continuation owner=%p want=%p pc=%#x", owner, consumer, consumerReturnPC)
			}
			for iteration := range 32 {
				got, err := consumer.Invoke("run")
				if err != nil || !slices.Equal(got, []uint64{58}) {
					t.Fatalf("iteration %d: run()=%v err=%v want=[58]", iteration, got, err)
				}
			}
		})
	}
}

func TestRV64RootedScalarTwoImportedTablesExecute(t *testing.T) {
	source := rootedScalarTwoImportedTablesModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("bounded rooted two-imported-table module was not source-admitted")
	}
	consumerCode, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	view := consumerCode.executionView()
	if !rv64FreshRootedGC(view) || view.tableImportCount() != 2 || view.tableCount() != 2 {
		t.Fatalf("two-imported-table receipt=%t imports=%d tables=%d validation=%v", rv64FreshRootedGC(view), view.tableImportCount(), view.tableCount(), railshotValidateCompiled(view))
	}
	providerCode, err := Compile(rv64RootedScalarConfig(), rootedScalarImportedTableProviderModule())
	if err != nil {
		t.Fatal(err)
	}
	defer providerCode.Close()
	rt := NewRuntime(WithRuntimeConfig(rv64RootedScalarConfig()))
	defer rt.Close()
	providerModule, err := rt.Module(providerCode)
	if err != nil {
		t.Fatal(err)
	}
	providers := make([]*Instance, 2)
	tables := make([]*Table, 2)
	for i := range providers {
		providers[i], err = rt.Instantiate(context.Background(), providerModule, WithGC(GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}))
		if err != nil {
			providerModule.Close()
			t.Fatal(err)
		}
		defer providers[i].Close()
		tables[i], err = providers[i].ExportedTable("table")
		if err != nil {
			providerModule.Close()
			t.Fatal(err)
		}
		defer tables[i].Close()
	}
	providerModule.Close()
	consumerModule, err := rt.Module(consumerCode)
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := rt.Instantiate(context.Background(), consumerModule,
		WithImports(NewImports().Table("env", "first", tables[0]).Table("env", "second", tables[1])),
		WithGC(GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}),
	)
	consumerModule.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	if consumer.gc == nil || providers[0].gc != consumer.gc || providers[1].gc != consumer.gc {
		t.Fatal("two imported tables did not join one Runtime GC domain")
	}
	for range 16 {
		if got, err := consumer.Invoke("run"); err != nil || !slices.Equal(got, []uint64{58}) {
			t.Fatalf("run()=%v err=%v want=[58]", got, err)
		}
	}
}

func TestRV64RootedScalarSecondImportedTableValidatedBeforePublication(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedScalarTwoImportedTablesModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	first, err := NewTable(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewTable(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	binary.LittleEndian.PutUint64(second.desc[8:], 1)
	instance, err := Instantiate(compiled, InstantiateOptions{
		Imports: NewImports().Table("env", "first", first).Table("env", "second", second),
		GC:      GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if instance != nil {
		_ = instance.Close()
		t.Fatal("malformed second imported table was published")
	}
	if err == nil || !strings.Contains(err.Error(), "imported table 1") {
		t.Fatalf("malformed second imported table error=%v", err)
	}
}

func TestRV64RootedScalarPublicTableHostTargetExecutes(t *testing.T) {
	source := rootedScalarPublicTableHostTargetModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("bounded rooted public-table host target was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	roots := view.genericGCFrameRoots()
	if !rv64FreshRootedGC(view) || view.NumImports != 1 || view.tableImportCount() != 0 || view.tableCount() != 1 || view.tableExports["table"] != 0 || roots == nil || len(roots.callsites) != 1 || len(roots.callsites[0].offsets) != 1 {
		t.Fatalf("public-table host receipt=%t imports=%d tables=%d/%d exports=%v roots=%+v validation=%v", rv64FreshRootedGC(view), view.NumImports, view.tableImportCount(), view.tableCount(), view.tableExports, roots, railshotValidateCompiled(view))
	}
	calls := 0
	host := slotHostFunc(func(module HostModule, _, results []uint64) {
		calls++
		gcModule, ok := module.(GCHostModule)
		if !ok {
			panic("rooted host module has no collector surface")
		}
		if err := gcModule.CollectGC(); err != nil {
			panic(err)
		}
		results[0] = 17
	})
	instance, err := Instantiate(compiled, InstantiateOptions{
		Imports: NewImports().Function("env", "host", host),
		GC:      GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for range 16 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{58}) {
			t.Fatalf("run()=%v err=%v want=[58]", got, err)
		}
	}
	if calls != 16 {
		t.Fatalf("host calls=%d want=16", calls)
	}
}

func rootedScalarPassiveTableModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	target := []byte{0x00, 0x41, 0x2a, 0x0b}
	caller := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x29, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x41, 0x00, 0x41, 0x00, 0x41, 0x01, 0xfc, 0x0c, 0x00, 0x00,
		0x41, 0x00, 0x11, 0x01, 0x00,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x6a,
		0xfc, 0x0d, 0x00,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(4, []byte{0x01, 0x70, 0x01, 0x01, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(9, []byte{0x01, 0x01, 0x00, 0x01, 0x00}),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(target))), target...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func TestRV64RootedScalarPassiveTableExecutes(t *testing.T) {
	source := rootedScalarPassiveTableModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("bounded rooted passive-table module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || !view.requiredFeatures.IsEnabled(CoreFeatureBulkMemoryOperations) || len(view.passiveElems) != 1 || view.passiveElems[0].Mode != ElemModePassive || len(view.passiveElems[0].Values) != 1 || view.passiveElems[0].Values[0].FuncIndex != 0 {
		t.Fatal("passive-table generic rooted receipt is missing")
	}
	for name, mutate := range map[string]func(*Compiled){
		"mode": func(candidate *Compiled) { candidate.passiveElems[0].Mode = ElemModeDeclarative },
		"target": func(candidate *Compiled) {
			candidate.passiveElems[0].Values[0].FuncIndex = 1
		},
		"offset": func(candidate *Compiled) { candidate.passiveElems[0].Offset.Base = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			candidate.passiveElems = slices.Clone(candidate.passiveElems)
			candidate.passiveElems[0].Values = slices.Clone(candidate.passiveElems[0].Values)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated passive table retained the generic receipt")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatalf("mutated passive table validation=%v", err)
			}
		})
	}
	first, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := first.Invoke("run"); err != nil || !slices.Equal(got, []uint64{83}) {
		_ = first.Close()
		t.Fatalf("first run()=%v err=%v want=[83]", got, err)
	}
	if got, err := first.Invoke("run"); err == nil {
		_ = first.Close()
		t.Fatalf("dropped passive segment rerun=%v without trap", got)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if got, err := second.Invoke("run"); err != nil || !slices.Equal(got, []uint64{83}) {
		t.Fatalf("fresh instance run()=%v err=%v want=[83]", got, err)
	}
}

func rootedScalarExpressionTableModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	target := []byte{0x00, 0x41, 0x2a, 0x0b}
	caller := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x29, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x41, 0x00, 0x41, 0x00, 0x41, 0x01, 0xfc, 0x0c, 0x00, 0x00,
		0x41, 0x00, 0x11, 0x01, 0x00,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x6a,
		0x0b,
	}
	nuller := []byte{
		0x00,
		0x41, 0x00, 0x41, 0x01, 0x41, 0x01, 0xfc, 0x0c, 0x00, 0x00,
		0x41, 0x00, 0x11, 0x01, 0x00,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(2))),
		wasmtest.Section(4, []byte{0x01, 0x70, 0x01, 0x01, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("null", byte(wasm.ExternFunc), 2),
		)),
		wasmtest.Section(9, wasmtest.Vec(tableTestPassiveElemExpr(tableTestRefFuncExpr(0), tableTestRefNullFuncExpr()))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(target))), target...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
			append(wasmtest.ULEB(uint32(len(nuller))), nuller...),
		)),
	)
}

func TestRV64RootedScalarExpressionTableExecutes(t *testing.T) {
	source := rootedScalarExpressionTableModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("bounded rooted expression-table module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.passiveElems) != 1 || !view.passiveElems[0].HasValueType ||
		len(view.passiveElems[0].Values) != 2 || view.passiveElems[0].Values[0].Null || view.passiveElems[0].Values[0].FuncIndex != 0 ||
		!view.passiveElems[0].Values[1].Null {
		t.Fatal("expression-table generic rooted receipt is missing")
	}
	for name, mutate := range map[string]func(*Compiled){
		"type": func(candidate *Compiled) {
			candidate.passiveElems[0].ValueTypeIndex = uint32(len(candidate.ValueTypes))
		},
		"null": func(candidate *Compiled) {
			candidate.passiveElems[0].Values[1].Null = false
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			candidate.passiveElems = slices.Clone(candidate.passiveElems)
			candidate.passiveElems[0].Values = slices.Clone(candidate.passiveElems[0].Values)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated expression table retained the generic receipt")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatalf("mutated expression table validation=%v", err)
			}
		})
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{83}) {
		t.Fatalf("run()=%v err=%v want=[83]", got, err)
	}
	if got, err := instance.Invoke("null"); err == nil {
		t.Fatalf("null()=%v without indirect-call trap", got)
	}
	if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{83}) {
		t.Fatalf("run() after null=%v err=%v want=[83]", got, err)
	}
}

func rootedScalarActiveDeclarativeExpressionTableModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	target := []byte{0x00, 0x41, 0x2a, 0x0b}
	caller := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x29, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0xfc, 0x0d, 0x01,
		0x41, 0x00, 0x11, 0x01, 0x00,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x6a,
		0x0b,
	}
	declarative := append([]byte{0x07, 0x70}, tableTestExprVec(tableTestRefNullFuncExpr())...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(4, []byte{0x01, 0x70, 0x01, 0x01, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(9, wasmtest.Vec(
			tableTestActiveElemExpr(0, tableTestRefFuncExpr(0)),
			declarative,
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(target))), target...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func TestRV64RootedScalarActiveDeclarativeExpressionTableExecutes(t *testing.T) {
	source := rootedScalarActiveDeclarativeExpressionTableModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("bounded rooted active/declarative expression-table module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Elems) != 1 || !view.Elems[0].HasValueType || len(view.Elems[0].Values) != 1 ||
		len(view.passiveElems) != 2 || view.passiveElems[0].Mode != ElemModeActive || !view.passiveElems[0].HasValueType || len(view.passiveElems[0].Values) != 0 ||
		view.passiveElems[1].Mode != ElemModeDeclarative || !view.passiveElems[1].HasValueType || len(view.passiveElems[1].Values) != 0 {
		t.Fatal("active/declarative expression-table state is missing")
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for range 2 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{83}) {
			t.Fatalf("run()=%v err=%v want=[83]", got, err)
		}
	}
}

func rootedScalarTableInitializerModule(expr []byte, size uint32) []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	target := []byte{0x00, 0x41, 0x2a, 0x0b}
	caller := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x29, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x41, 0x00, 0x11, 0x01, 0x00,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x6a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(4, wasmtest.Vec(tableTestTableWithInit(size, 1, expr))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(target))), target...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func TestRV64RootedScalarTableInitializersExecute(t *testing.T) {
	nonNullSource := rootedScalarTableInitializerModule(tableTestRefFuncExpr(0), 1)
	if !railshot.AdmitGCRootedScalarSource(nonNullSource) {
		t.Fatal("bounded rooted ref.func table initializer was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), nonNullSource)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || !view.HasTableInitFunc || view.TableInitFunc != 0 || len(view.Elems) != 0 {
		t.Fatal("ref.func table initializer receipt is missing")
	}
	for name, mutate := range map[string]func(*Compiled){
		"disabled": func(candidate *Compiled) { candidate.HasTableInitFunc = false },
		"target":   func(candidate *Compiled) { candidate.TableInitFunc = 1 },
		"bounds": func(candidate *Compiled) {
			candidate.TableInitFunc = uint32(len(candidate.Funcs))
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated table initializer retained the generic receipt")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatalf("mutated table initializer validation=%v", err)
			}
		})
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{83}) {
		_ = instance.Close()
		t.Fatalf("ref.func-initialized run()=%v err=%v want=[83]", got, err)
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}

	nullSource := rootedScalarTableInitializerModule(tableTestRefNullFuncExpr(), 1)
	if !railshot.AdmitGCRootedScalarSource(nullSource) {
		t.Fatal("bounded rooted ref.null table initializer was not source-admitted")
	}
	nullCompiled, err := Compile(rv64RootedScalarConfig(), nullSource)
	if err != nil {
		t.Fatal(err)
	}
	defer nullCompiled.Close()
	if view := nullCompiled.executionView(); !rv64FreshRootedGC(view) || view.HasTableInitFunc || view.TableInitFunc != 0 {
		t.Fatal("ref.null table initializer receipt is missing")
	}
	nullInstance, err := Instantiate(nullCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer nullInstance.Close()
	if got, err := nullInstance.Invoke("run"); err == nil {
		t.Fatalf("ref.null-initialized run()=%v without indirect-call trap", got)
	}

	zeroCompiled, err := Compile(rv64RootedScalarConfig(), rootedScalarTableInitializerModule(tableTestRefFuncExpr(0), 0))
	if err != nil {
		t.Fatal(err)
	}
	defer zeroCompiled.Close()
	if view := zeroCompiled.executionView(); !rv64FreshRootedGC(view) || !view.HasTableInitFunc || view.TableInitFunc != 0 || view.TableSize != 0 {
		t.Fatal("zero-length ref.func table initializer receipt is missing")
	}
	zeroInstance, err := Instantiate(zeroCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer zeroInstance.Close()
	if got, err := zeroInstance.Invoke("run"); err == nil {
		t.Fatalf("zero-length ref.func-initialized run()=%v without bounds trap", got)
	}
}

func rootedScalarTwoTableModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	target := []byte{0x00, 0x41, 0x2a, 0x0b}
	caller := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x29, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x41, 0x00, 0x11, 0x01, 0x01,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x6a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(4, wasmtest.Vec(
			[]byte{0x70, 0x01, 0x01, 0x01},
			tableTestTableWithInit(1, 1, tableTestRefFuncExpr(0)),
		)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(9, wasmtest.Vec(tableTestActiveElemAt(0, 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(target))), target...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func TestRV64RootedScalarTwoTablesExecute(t *testing.T) {
	source := rootedScalarTwoTableModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("bounded rooted two-table module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || view.tableCount() != 2 || len(view.extraTables) != 1 || !view.extraTables[0].HasInitFunc || view.extraTables[0].InitFunc != 0 || len(view.Elems) != 1 || view.Elems[0].TableIndex != 0 {
		t.Fatal("two-table generic rooted receipt is missing")
	}
	for name, mutate := range map[string]func(*Compiled){
		"capacity":    func(candidate *Compiled) { candidate.extraTables[0].Max = 2 },
		"initializer": func(candidate *Compiled) { candidate.extraTables[0].HasInitFunc = false },
		"target":      func(candidate *Compiled) { candidate.Elems[0].TableIndex = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			candidate.extraTables = slices.Clone(candidate.extraTables)
			candidate.Elems = slices.Clone(candidate.Elems)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated two-table directory retained the generic receipt")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatalf("mutated two-table validation=%v", err)
			}
		})
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for range 2 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{83}) {
			t.Fatalf("run()=%v err=%v want=[83]", got, err)
		}
	}
}

func rootedScalarDeclarativeTableModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	target := []byte{0x00, 0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a, 0x41, 0x2a, 0x0b}
	caller := []byte{0x00, 0xfc, 0x0d, 0x01, 0x41, 0x00, 0x11, 0x01, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(4, []byte{0x01, 0x70, 0x01, 0x01, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(9, []byte{
			0x02,
			0x00, 0x41, 0x00, 0x0b, 0x01, 0x00,
			0x03, 0x00, 0x01, 0x00,
		}),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(target))), target...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func rootedScalarImportedStatePrivateTableModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	globalImport := wasmtest.GlobalImportEntry("env", "base", wasm.I32, false)
	memoryImport := append(append(wasmtest.Name("env.state"), wasmtest.Name("memory.0")...), byte(wasm.ExternMem), 0x01, 0x01, 0x01)
	target := []byte{0x00, 0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a, 0x23, 0x00, 0x0b}
	caller := []byte{0x00, 0x41, 0x00, 0x11, 0x01, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(globalImport, memoryImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(4, []byte{0x01, 0x70, 0x01, 0x01, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("base", byte(wasm.ExternGlobal), 0),
			wasmtest.ExportEntry("memory", byte(wasm.ExternMem), 0),
		)),
		wasmtest.Section(9, []byte{0x01, 0x00, 0x41, 0x00, 0x0b, 0x01, 0x00}),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(target))), target...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func TestRV64RootedScalarDeclarativeTableExecutes(t *testing.T) {
	source := rootedScalarDeclarativeTableModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("bounded rooted declarative-table module was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Elems) != 1 || len(view.passiveElems) != 2 || view.passiveElems[0].Mode != ElemModeActive || len(view.passiveElems[0].Values) != 0 || view.passiveElems[1].Mode != ElemModeDeclarative || len(view.passiveElems[1].Values) != 0 {
		digest, valid := rv64RootedTableDigest(view)
		t.Fatalf("declarative-table original-index state is missing: fresh=%t validation=%v digest=%t/%t active=%+v state=%+v", rv64FreshRootedGC(view), railshotValidateCompiled(view), valid, digest == view.validateMemo.rv64FreshRootedGCTableDigest, view.Elems, view.passiveElems)
	}
	for range 2 {
		instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{42}) {
				_ = instance.Close()
				t.Fatalf("run()=%v err=%v want=[42]", got, err)
			}
		}
		if err := instance.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRV64RootedScalarImportedStatePrivateTableExecutes(t *testing.T) {
	source := rootedScalarImportedStatePrivateTableModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("rooted imported state with a private function table was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || view.NumImports != 0 || len(view.GlobalImports) != 1 || view.memoryImport == "" || view.tableCount() != 1 {
		t.Fatalf("rooted imported-state table receipt=%t funcs=%d globals=%d memory=%q tables=%d validation=%v", rv64FreshRootedGC(view), view.NumImports, len(view.GlobalImports), view.memoryImport, view.tableCount(), railshotValidateCompiled(view))
	}
	base := NewGlobalI32(42, false)
	defer base.Close()
	memory, err := NewMemory(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{
		Imports: NewImports().Global("env", "base", base).Memory("env.state", "memory.0", memory),
		GC:      GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for range 4 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{42}) {
			t.Fatalf("run()=%v err=%v want=[42]", got, err)
		}
	}
	if exported, err := instance.ExportedMemory("memory"); err != nil || exported != memory {
		t.Fatalf("private-table memory re-export lost identity: exported=%p want=%p err=%v", exported, memory, err)
	}
	if exported, err := instance.ExportedGlobalObject("base"); err != nil || exported != base {
		t.Fatalf("private-table global re-export lost identity: exported=%p want=%p err=%v", exported, base, err)
	}
}

func rootedScalarHostImportModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	importEntry := append(append(wasmtest.Name("env.calls"), wasmtest.Name("host.fn")...), byte(wasm.ExternFunc))
	importEntry = append(importEntry, wasmtest.ULEB(1)...)
	body := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x29, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x41, 0x29, 0x10, 0x00,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x6a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rootedScalarHostImportPrivateTableModule(tableTarget uint32) []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	importEntry := append(append(wasmtest.Name("env.calls"), wasmtest.Name("host.fn")...), byte(wasm.ExternFunc))
	importEntry = append(importEntry, wasmtest.ULEB(1)...)
	target := []byte{0x00, 0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a, 0x41, 0x2a, 0x0b}
	caller := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x29, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x41, 0x29, 0x10, 0x00,
		0x41, 0x00, 0x11, 0x02, 0x00, 0x6a,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x6a,
		0x0b,
	}
	element := []byte{0x01, 0x00, 0x41, 0x00, 0x0b, 0x01}
	element = append(element, wasmtest.ULEB(tableTarget)...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(2))),
		wasmtest.Section(4, []byte{0x01, 0x70, 0x01, 0x01, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 2))),
		wasmtest.Section(9, element),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(target))), target...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func rootedScalarHostImportMixedTableModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	functionImport := append(append(wasmtest.Name("env.calls"), wasmtest.Name("host.fn")...), byte(wasm.ExternFunc))
	functionImport = append(functionImport, wasmtest.ULEB(1)...)
	tableImport := append(append(wasmtest.Name("env.tables"), wasmtest.Name("public")...), byte(wasm.ExternTable), 0x70, 0x01, 0x01, 0x01)
	target := []byte{0x00, 0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a, 0x41, 0x2a, 0x0b}
	caller := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x29, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x41, 0x00, 0x11, 0x02, 0x01,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x6a,
		0x0b,
	}
	// Active segment for table index 1, initialized with imported function 0.
	element := []byte{0x01, 0x02, 0x01, 0x41, 0x00, 0x0b, 0x00, 0x01, 0x00}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(functionImport, tableImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(2))),
		wasmtest.Section(4, []byte{0x01, 0x70, 0x01, 0x01, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 2))),
		wasmtest.Section(9, element),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(target))), target...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func rootedScalarHostImportArityModule(arity int) []byte {
	params := make([]wasm.ValType, arity)
	for i := range params {
		params[i] = wasm.I32
	}
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(params, nil),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	importEntry := append(append(wasmtest.Name("env"), wasmtest.Name("host")...), byte(wasm.ExternFunc))
	importEntry = append(importEntry, wasmtest.ULEB(1)...)
	body := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x29, 0xfb, 0x00, 0x00, 0x21, 0x00,
	}
	for range arity {
		body = append(body, 0x41, 0x00)
	}
	body = append(body, 0x10, 0x00, 0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rootedScalarHostImportCountModule(count int) []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, nil),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	imports := make([][]byte, count)
	for i := range imports {
		entry := append(append(wasmtest.Name("env"), wasmtest.Name(fmt.Sprintf("host%d", i))...), byte(wasm.ExternFunc))
		imports[i] = append(entry, wasmtest.ULEB(1)...)
	}
	body := []byte{0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(imports...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), uint32(count)))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rootedScalarImportedStartModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, nil),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	globalImport := append(append(wasmtest.Name("env"), wasmtest.Name("base")...), byte(wasm.ExternGlobal), 0x7f, 0x00)
	importEntry := append(append(wasmtest.Name("env"), wasmtest.Name("start")...), byte(wasm.ExternFunc))
	importEntry = append(importEntry, wasmtest.ULEB(1)...)
	body := []byte{0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(globalImport, importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(8, wasmtest.ULEB(0)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rootedScalarLocalStartWithImportModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, nil),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	importEntry := append(append(wasmtest.Name("env"), wasmtest.Name("unused")...), byte(wasm.ExternFunc))
	importEntry = append(importEntry, wasmtest.ULEB(1)...)
	start := []byte{0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0x1a, 0x41, 0x07, 0x24, 0x00, 0x0b}
	run := []byte{0x00, 0x23, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2))),
		wasmtest.Section(6, []byte{0x01, 0x7f, 0x01, 0x41, 0x00, 0x0b}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 2))),
		wasmtest.Section(8, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(start))), start...),
			append(wasmtest.ULEB(uint32(len(run))), run...),
		)),
	)
}

func rootedScalarNumericGlobalImportModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}),
	}
	globalImport := func(name string, typ wasm.ValType, mutable bool) []byte {
		entry := append(append(wasmtest.Name("env"), wasmtest.Name(name)...), byte(wasm.ExternGlobal))
		mut := byte(0)
		if mutable {
			mut = 1
		}
		return append(entry, wasm.MustEncodeValType(typ), mut)
	}
	body := []byte{
		0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00, 0x1a,
		0x23, 0x00, 0x41, 0x01, 0x6a, 0x24, 0x00,
		0x23, 0x00, 0x23, 0x01, 0x23, 0x02, 0x23, 0x03,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(
			globalImport("i32", wasm.I32, true),
			globalImport("i64", wasm.I64, false),
			globalImport("f32", wasm.F32, false),
			globalImport("f64", wasm.F64, false),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("cell", byte(wasm.ExternGlobal), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rootedScalarMutableStructGlobalModule(exportGlobal bool) []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	global := []byte{0x01, 0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b}
	body := []byte{
		0x00,
		0x41, 0x29, 0xfb, 0x00, 0x00, 0x24, 0x00,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		0x23, 0x00, 0xfb, 0x02, 0x00, 0x00,
		0x41, 0x01, 0x6a,
		0x0b,
	}
	trap := []byte{0x00, 0x41, 0x37, 0xfb, 0x00, 0x00, 0x24, 0x00, 0x00, 0x41, 0x00, 0x0b}
	get := []byte{0x00, 0x23, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	exports := [][]byte{
		wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0),
		wasmtest.ExportEntry("store_then_trap", byte(wasm.ExternFunc), 1),
		wasmtest.ExportEntry("get", byte(wasm.ExternFunc), 2),
	}
	if exportGlobal {
		exports = append(exports, wasmtest.ExportEntry("root", byte(wasm.ExternGlobal), 0))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(6, global),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(body))), body...),
			append(wasmtest.ULEB(uint32(len(trap))), trap...),
			append(wasmtest.ULEB(uint32(len(get))), get...),
		)),
	)
}

func rootedScalarMutableArrayGlobalModule() []byte {
	types := [][]byte{
		{0x5e, 0x7f, 0x01},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	global := []byte{0x01, 0x63, 0x00, 0x01, 0xd0, 0x00, 0x0b}
	body := []byte{
		0x00,
		0x41, 0x29, 0x41, 0x01, 0xfb, 0x06, 0x00, 0x24, 0x00,
		0x41, 0x07, 0x41, 0x01, 0xfb, 0x06, 0x00, 0x1a,
		0x23, 0x00, 0x41, 0x00, 0xfb, 0x0b, 0x00,
		0x41, 0x01, 0x6a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, global),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rootedScalarI31GlobalModule() []byte {
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

func rootedScalarPublicImmutableReferenceGlobalsModule() []byte {
	read := []byte{0x00, 0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b}
	structGlobal := []byte{0x64, 0x00, 0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0x0b}
	i31Global := wasmtest.GlobalEntry(wasm.I31Ref, false, []byte{0x41, 0x7e, 0xfb, 0x1c, 0x0b})
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			[]byte{0x60, 0x01, 0x64, 0x00, 0x01, 0x7f},
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(structGlobal, i31Global)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("read", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("object", byte(wasm.ExternGlobal), 0),
			wasmtest.ExportEntry("immediate", byte(wasm.ExternGlobal), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(read))), read...))),
	)
}

func rootedScalarSharedGlobalProviderModule(mutable bool) []byte {
	mut := byte(0)
	if mutable {
		mut = 1
	}
	global := []byte{
		0x63, 0x00, mut, // (global (ref null 0))
		0x41, 0x2a, 0xfb, 0x00, 0x00,
		0x0b,
	}
	body := []byte{
		0x00,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		0x23, 0x00, 0xd4, 0xfb, 0x02, 0x00, 0x00,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("g", byte(wasm.ExternGlobal), 0),
			wasmtest.ExportEntry("read", byte(wasm.ExternFunc), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func rootedScalarSharedGlobalConsumerModule(mutable bool) []byte {
	mut := byte(0)
	if mutable {
		mut = 1
	}
	globalImport := append(append(wasmtest.Name("provider"), wasmtest.Name("g")...), byte(wasm.ExternGlobal), 0x63, 0x00, mut)
	body := []byte{0x00}
	if mutable {
		body = append(body,
			0x41, 0xcd, 0x00, 0xfb, 0x00, 0x00,
			0x24, 0x00,
		)
	}
	body = append(body,
		0x41, 0x09, 0xfb, 0x00, 0x00, 0x1a,
		0x23, 0x00, 0xd4, 0xfb, 0x02, 0x00, 0x00,
		0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(globalImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("g", byte(wasm.ExternGlobal), 0),
			wasmtest.ExportEntry("read", byte(wasm.ExternFunc), 0),
		)),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedSharedReferenceGlobalImports(t *testing.T) {
	for _, mutable := range []bool{false, true} {
		t.Run(map[bool]string{false: "immutable", true: "mutable"}[mutable], func(t *testing.T) {
			providerSource := rootedScalarSharedGlobalProviderModule(mutable)
			consumerSource := rootedScalarSharedGlobalConsumerModule(mutable)
			if !railshot.AdmitGCRootedScalarSource(providerSource) || !railshot.AdmitGCRootedScalarSource(consumerSource) {
				t.Fatal("shared collector-reference globals were not source-admitted")
			}
			consumerModule, err := wasm.DecodeModule(consumerSource)
			if err != nil {
				t.Fatal(err)
			}
			var rootDiagnostic string
			if plan := newGCFrameRootPlan(consumerModule, true, true, &rootDiagnostic, nil); plan == nil || rootDiagnostic != "" {
				t.Fatalf("shared collector-reference global root plan=%v diagnostic=%q", plan, rootDiagnostic)
			}
			providerBase, err := Compile(rv64RootedScalarConfig(), providerSource)
			if err != nil {
				t.Fatal(err)
			}
			defer providerBase.Close()
			consumerBase, err := Compile(rv64RootedScalarConfig(), consumerSource)
			if err != nil {
				t.Fatal(err)
			}
			defer consumerBase.Close()
			if !rv64FreshRootedGC(providerBase.executionView()) || !rv64FreshRootedGC(consumerBase.executionView()) {
				t.Fatalf("shared global receipts provider=%t consumer=%t", rv64FreshRootedGC(providerBase.executionView()), rv64FreshRootedGC(consumerBase.executionView()))
			}
			candidate := cloneCompiledMetadata(consumerBase.executionView())
			candidate.GlobalImports = slices.Clone(candidate.GlobalImports)
			candidate.GlobalImports[0].Mutable = !mutable
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated collector-reference global import retained the rooted receipt")
			}

			for _, artifact := range []bool{false, true} {
				t.Run(map[bool]string{false: "compiled", true: "artifact"}[artifact], func(t *testing.T) {
					providerCode, consumerCode := providerBase, consumerBase
					if artifact {
						providerCode = publicArtifactRoundTrip(t, providerBase)
						consumerCode = publicArtifactRoundTrip(t, consumerBase)
						defer providerCode.Close()
						defer consumerCode.Close()
					}
					store := newReferenceStore(false)
					defer store.closeRuntime()
					gcConfig := GCConfig{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}
					provider, err := instantiateCore(providerCode, InstantiateOptions{GC: gcConfig, store: store})
					if err != nil {
						t.Fatal(err)
					}
					global, err := provider.ExportedGlobalObject("g")
					if err != nil {
						_ = provider.Close()
						t.Fatal(err)
					}
					consumer, err := instantiateCore(consumerCode, InstantiateOptions{GC: gcConfig, store: store, Imports: testImports("provider.g", global)})
					if err != nil {
						_ = provider.Close()
						t.Fatal(err)
					}
					want := uint64(42)
					if mutable {
						want = 77
					}
					if provider.gc == nil || provider.gc != consumer.gc || !store.ownsGCCollector(provider.gc) {
						t.Fatal("shared reference globals did not select one Runtime collector domain")
					}
					if got, callErr := consumer.Invoke("read"); callErr != nil || !slices.Equal(got, []uint64{want}) {
						t.Fatalf("consumer read=%v err=%v want=%d", got, callErr, want)
					}
					if got, callErr := provider.Invoke("read"); callErr != nil || !slices.Equal(got, []uint64{want}) {
						t.Fatalf("provider read=%v err=%v want=%d", got, callErr, want)
					}
					if err := consumer.CollectGC(); err != nil {
						t.Fatal(err)
					}
					if err := provider.Close(); err != nil {
						t.Fatal(err)
					}
					if err := consumer.CollectGC(); err != nil {
						t.Fatal(err)
					}
					if got, callErr := consumer.Invoke("read"); callErr != nil || !slices.Equal(got, []uint64{want}) {
						t.Fatalf("retained global after provider close=%v err=%v want=%d", got, callErr, want)
					}
					if err := consumer.Close(); err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}

func TestRV64RootedImmutableReferenceGlobalsExport(t *testing.T) {
	source := rootedScalarPublicImmutableReferenceGlobalsModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("immutable collector-reference globals were not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || view.GlobalExports["object"] != 0 || view.GlobalExports["immediate"] != 1 {
		t.Fatalf("immutable global receipt=%t exports=%v validation=%v", rv64FreshRootedGC(view), view.GlobalExports, railshotValidateCompiled(view))
	}
	for name, mutate := range map[string]func(*Compiled){
		"namespace": func(candidate *Compiled) {
			candidate.GlobalExports = cloneExportMap(candidate.GlobalExports)
			delete(candidate.GlobalExports, "object")
			candidate.GlobalExports["renamed"] = 0
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated public reference global retained the rooted receipt")
			}
		})
	}
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	object, err := in.GlobalValue("object")
	if err != nil || object.Type() != ValAnyRef || object.GCRef().IsNull() || object.Bits() == uint64(uint32(object.Bits())) {
		t.Fatalf("object global=%v err=%v; want opaque aggregate token", object, err)
	}
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
	if got, err := in.InvokeValues(context.Background(), "read", ValueGCRef(object.GCRef())); err != nil || len(got) != 1 || got[0].I32() != 42 {
		t.Fatalf("read(object global)=%v err=%v", got, err)
	}
	immediate, err := in.GlobalValue("immediate")
	if err != nil || immediate.Type() != ValI31Ref || immediate.I31Ref().Signed() != -2 || immediate.Bits()>>32 != 0 {
		t.Fatalf("i31 global=%v err=%v", immediate, err)
	}
	if err := in.ReleaseGCRef(object.GCRef()); err != nil {
		t.Fatal(err)
	}
	if _, err := in.InvokeValues(context.Background(), "read", ValueGCRef(object.GCRef())); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale global token ingress=%v", err)
	}
}

func TestRV64RootedScalarMutableStructGlobalExecutes(t *testing.T) {
	source := rootedScalarMutableStructGlobalModule(true)
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("generic rooted mutable struct global export was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Globals) != 1 || view.Globals[0].Type != ValAnyRef || !view.Globals[0].Mutable {
		t.Fatalf("rooted mutable-global receipt=%t globals=%+v validation=%v", rv64FreshRootedGC(view), view.Globals, railshotValidateCompiled(view))
	}
	candidate := cloneCompiledMetadata(view)
	candidate.ValueTypes = slices.Clone(candidate.ValueTypes)
	candidate.ValueTypes[candidate.Globals[0].ValueTypeIndex].Ref.Heap.TypeIndex = 1
	if rv64FreshRootedGC(candidate) {
		t.Fatal("retargeted rooted global exact type retained the generic receipt")
	}
	instance, err := Instantiate(compiled, InstantiateOptions{
		GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if roots := instance.publicGCState().globalRoots; len(roots) != 1 || roots[0].GlobalIndex != 0 {
		t.Fatalf("generic rooted mutable-global roots=%+v", roots)
	}
	for range 16 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{42}) {
			t.Fatalf("run()=%v err=%v want=[42]", got, err)
		}
	}
	root, err := instance.GlobalValue("root")
	if err != nil || root.Type() != ValAnyRef || root.GCRef().IsNull() || root.Bits() == uint64(uint32(root.Bits())) {
		t.Fatalf("mutable root global=%v err=%v; want opaque aggregate token", root, err)
	}
	if _, err := instance.Invoke("run"); err != nil {
		t.Fatal(err)
	}
	if err := instance.SetGlobalValue("root", ValueGCRef(root.GCRef())); err != nil {
		t.Fatal(err)
	}
	if err := instance.ReleaseGCRef(root.GCRef()); err != nil {
		t.Fatal(err)
	}
	if err := instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
	if got, err := instance.Invoke("get"); err != nil || !slices.Equal(got, []uint64{41}) {
		t.Fatalf("get() after public global restore=%v err=%v want=[41]", got, err)
	}
	if err := instance.SetGlobalValue("root", ValueGCRef(root.GCRef())); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale global token ingress=%v", err)
	}
	if err := instance.SetGlobalValue("root", ValueGCRef(NullGCRef())); err != nil {
		t.Fatal(err)
	}
	if value, err := instance.GlobalValue("root"); err != nil || !value.GCRef().IsNull() {
		t.Fatalf("cleared mutable root=%v err=%v", value, err)
	}
	resolvedTrap, err := instance.WasmFunc("store_then_trap")
	if err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() ([]uint64, error){
		"ordinary": func() ([]uint64, error) { return instance.Invoke("store_then_trap") },
		"resolved": func() ([]uint64, error) { return resolvedTrap.Invoke() },
	} {
		if got, err := call(); err == nil || got != nil {
			t.Fatalf("%s store_then_trap()=%v err=%v, want trap", name, got, err)
		}
		// Model a different tenant collecting immediately after the trapping call's
		// complete-call lease is released. Instance.CollectGC would first synchronize
		// this instance and would therefore hide a failed trap-path publication.
		lease := instance.lockGCInvocation(newInvocationID())
		domain := instance.lockGCCollector()
		err = instance.gc.CollectFull(native.EmptyRoots{})
		unlockGCCollector(domain)
		lease.unlock()
		if err != nil {
			t.Fatal(err)
		}
		if got, err := instance.Invoke("get"); err != nil || !slices.Equal(got, []uint64{55}) {
			t.Fatalf("%s get() after trapped store and foreign collection=%v err=%v want=[55]", name, got, err)
		}
	}
}

func TestRV64RootedScalarMutableArrayGlobalExecutes(t *testing.T) {
	source := rootedScalarMutableArrayGlobalModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("generic rooted mutable array global was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Globals) != 1 || view.Globals[0].Type != ValAnyRef || !view.Globals[0].Mutable {
		t.Fatalf("rooted mutable-array-global receipt=%t globals=%+v validation=%v", rv64FreshRootedGC(view), view.Globals, railshotValidateCompiled(view))
	}
	instance, err := Instantiate(compiled, InstantiateOptions{
		GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if roots := instance.publicGCState().globalRoots; len(roots) != 1 || roots[0].GlobalIndex != 0 {
		t.Fatalf("generic rooted mutable-array-global roots=%+v", roots)
	}
	for range 16 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{42}) {
			t.Fatalf("run()=%v err=%v want=[42]", got, err)
		}
	}
}

func TestRV64RootedScalarI31GlobalExecutes(t *testing.T) {
	source := rootedScalarI31GlobalModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("generic rooted i31 global was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.Globals) != 1 || view.Globals[0].Type != ValI31Ref || !view.Globals[0].Mutable {
		t.Fatalf("rooted i31-global receipt=%t globals=%+v validation=%v", rv64FreshRootedGC(view), view.Globals, railshotValidateCompiled(view))
	}
	candidate := cloneCompiledMetadata(view)
	candidate.ValueTypes = slices.Clone(candidate.ValueTypes)
	candidate.ValueTypes[candidate.Globals[0].ValueTypeIndex].Ref.Heap.Abstract = AbstractHeapAny
	candidate.Globals = slices.Clone(candidate.Globals)
	candidate.Globals[0].Type = ValAnyRef
	if rv64FreshRootedGC(candidate) {
		t.Fatal("retargeted abstract-any global retained the generic rooted receipt")
	}
	instance, err := Instantiate(compiled, InstantiateOptions{
		GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if roots := instance.publicGCState().globalRoots; len(roots) != 1 || roots[0].GlobalIndex != 0 {
		t.Fatalf("generic rooted i31-global roots=%+v", roots)
	}
	for range 16 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{0xfffffffe}) {
			t.Fatalf("run()=%v err=%v want=[4294967294]", got, err)
		}
	}
	if err := instance.SetGlobalValue("root", ValueI31Ref(NewI31Ref(-3))); err != nil {
		t.Fatal(err)
	}
	if value, err := instance.GlobalValue("root"); err != nil || value.Type() != ValI31Ref || value.I31Ref().Signed() != -3 || value.Bits()>>32 != 0 {
		t.Fatalf("mutable i31 global=%v err=%v", value, err)
	}
	if err := instance.SetGlobalValue("root", ValueOf(ValI31Ref, 0xdeadbeef00000003)); err == nil || !strings.Contains(err.Error(), "invalid i31ref") {
		t.Fatalf("dirty-upper i31 global ingress=%v", err)
	}
}

func rootedScalarI31TableModule(exportTable bool) []byte {
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

func TestRV64RootedScalarI31TableExecutes(t *testing.T) {
	source := rootedScalarI31TableModule(false)
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("generic rooted i31 table was not source-admitted")
	}
	if !railshot.AdmitGCRootedScalarSource(rootedScalarI31TableModule(true)) {
		t.Fatal("public generic rooted i31 table was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || view.tableCount() != 1 || view.tableElementType(0) != ValI31Ref || view.tableEntryBytes(0) != 8 {
		t.Fatalf("rooted i31-table receipt=%t tables=%d type=%v stride=%d validation=%v", rv64FreshRootedGC(view), view.tableCount(), view.tableElementType(0), view.tableEntryBytes(0), railshotValidateCompiled(view))
	}
	candidate := cloneCompiledMetadata(view)
	candidate.ValueTypes = slices.Clone(candidate.ValueTypes)
	candidate.ValueTypes[candidate.tableDef(0).ValueTypeIndex].Ref.Heap.Abstract = AbstractHeapAny
	candidate.TableType = ValAnyRef
	if rv64FreshRootedGC(candidate) {
		t.Fatal("retargeted abstract-any table retained the generic rooted receipt")
	}
	for iteration := range 8 {
		instance, err := Instantiate(compiled, InstantiateOptions{
			GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		want := []uint64{0xfffffffe, 0xfffffffd, 2, 0xfffffffc, 0xfffffffc}
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, want) {
			_ = instance.Close()
			t.Fatalf("iteration %d: run()=%v err=%v want=%v", iteration, got, err, want)
		}
		if err := instance.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRV64RootedScalarI31TableExportExecutes(t *testing.T) {
	compiled, err := Compile(rv64RootedScalarConfig(), rootedScalarI31TableModule(true))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || view.tableExports["values"] != 0 || view.tableElementType(0) != ValI31Ref || view.tableEntryBytes(0) != 8 {
		t.Fatalf("public i31-table receipt=%t exports=%v type=%v stride=%d validation=%v", rv64FreshRootedGC(view), view.tableExports, view.tableElementType(0), view.tableEntryBytes(0), railshotValidateCompiled(view))
	}
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	table, err := instance.ExportedTable("values")
	if err != nil {
		t.Fatal(err)
	}
	if table.owner == nil || table.owner.elementType != ValI31Ref || !table.owner.hasValueType || table.owner.store != instance.refStore || table.Size() != 2 {
		t.Fatalf("public i31-table handle=%+v owner=%+v", table, table.owner)
	}
	want := []uint64{0xfffffffe, 0xfffffffd, 2, 0xfffffffc, 0xfffffffc}
	if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, want) {
		t.Fatalf("run()=%v err=%v want=%v", got, err, want)
	}
	if err := instance.CollectGC(); err != nil {
		t.Fatal(err)
	}
}

func rootedScalarI31PassiveElementModule() []byte {
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

func TestRV64RootedScalarI31PassiveElementExecutes(t *testing.T) {
	source := rootedScalarI31PassiveElementModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("generic rooted i31 passive element segment was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.passiveElems) != 1 || view.passiveElems[0].RefType != ValI31Ref || len(view.passiveElems[0].Values) != 2 {
		t.Fatalf("rooted i31-element receipt=%t passive=%+v validation=%v", rv64FreshRootedGC(view), view.passiveElems, railshotValidateCompiled(view))
	}
	candidate := cloneCompiledMetadata(view)
	candidate.passiveElems = slices.Clone(candidate.passiveElems)
	candidate.passiveElems[0].Values = slices.Clone(candidate.passiveElems[0].Values)
	candidate.passiveElems[0].Values[0].FuncIndex &^= 1
	if rv64FreshRootedGC(candidate) {
		t.Fatal("even i31 element immediate retained the generic rooted receipt")
	}
	for iteration := range 8 {
		instance, err := Instantiate(compiled, InstantiateOptions{
			GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		want := []uint64{0xfffffffe, 0xfffffffd}
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, want) {
			_ = instance.Close()
			t.Fatalf("iteration %d: run()=%v err=%v want=%v", iteration, got, err, want)
		}
		if err := instance.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func rootedScalarMixedFunctionGlobalImportModule() []byte {
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	globalImport := append(append(wasmtest.Name("env"), wasmtest.Name("base")...), byte(wasm.ExternGlobal), 0x7f, 0x00)
	functionImport := append(append(wasmtest.Name("env"), wasmtest.Name("host")...), byte(wasm.ExternFunc))
	functionImport = append(functionImport, wasmtest.ULEB(1)...)
	body := []byte{
		0x01, 0x01, 0x63, 0x00,
		0x41, 0x29, 0xfb, 0x00, 0x00, 0x21, 0x00,
		0x23, 0x00, 0x10, 0x00,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x6a,
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(globalImport, functionImport)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64RootedScalarHostImportArityBound(t *testing.T) {
	bounded := rootedScalarHostImportArityModule(64)
	if !railshot.AdmitGCRootedScalarSource(bounded) {
		t.Fatal("64-slot rooted numeric host import was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), bounded)
	if err != nil {
		t.Fatal(err)
	}
	if view := compiled.executionView(); !rv64FreshRootedGC(view) || view.genericGCFrameRoots() == nil {
		_ = compiled.Close()
		t.Fatal("64-slot rooted numeric host import lost its exact root plan")
	}
	if err := compiled.Close(); err != nil {
		t.Fatal(err)
	}

	oversized := rootedScalarHostImportArityModule(65)
	if railshot.AdmitGCRootedScalarSource(oversized) {
		t.Fatal("65-slot rooted numeric host import was source-admitted")
	}
	if candidate, err := Compile(rv64RootedScalarConfig(), oversized); err == nil || candidate != nil {
		if candidate != nil {
			_ = candidate.Close()
		}
		t.Fatal("65-slot rooted numeric host import compiled")
	}
}

func TestRV64RootedScalarHostImportCountBound(t *testing.T) {
	bounded := rootedScalarHostImportCountModule(railshot.MaxHostFunctionImports)
	if !railshot.AdmitGCRootedScalarSource(bounded) {
		t.Fatal("maximum rooted numeric host import count was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), bounded)
	if err != nil {
		t.Fatal(err)
	}
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || view.NumImports != railshot.MaxHostFunctionImports || view.genericGCFrameRoots() == nil {
		_ = compiled.Close()
		t.Fatalf("maximum rooted host boundary lost its receipt: fresh=%t imports=%d", rv64FreshRootedGC(view), view.NumImports)
	}
	if err := compiled.Close(); err != nil {
		t.Fatal(err)
	}
	if railshot.AdmitGCRootedScalarSource(rootedScalarHostImportCountModule(railshot.MaxHostFunctionImports + 1)) {
		t.Fatal("oversized rooted numeric host import count was source-admitted")
	}
}

func TestRV64RootedScalarImportedStartExecutes(t *testing.T) {
	source := rootedScalarImportedStartModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("rooted numeric imported start was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || !view.HasStart || !view.StartIsImport || view.StartImportIdx != 0 || len(view.GlobalImports) != 1 {
		t.Fatalf("rooted imported start receipt=%t start=%t imported=%t index=%d validation=%v", rv64FreshRootedGC(view), view.HasStart, view.StartIsImport, view.StartImportIdx, railshotValidateCompiled(view))
	}
	for name, mutate := range map[string]func(*Compiled){
		"kind":  func(candidate *Compiled) { candidate.StartIsImport = false },
		"index": func(candidate *Compiled) { candidate.StartImportIdx = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated imported start retained the generic receipt")
			}
		})
	}
	starts := 0
	start := slotHostFunc(func(module HostModule, _, _ []uint64) {
		starts++
		gcModule, ok := module.(GCHostModule)
		if !ok {
			panic("rooted imported start has no collector surface")
		}
		if err := gcModule.CollectGC(); err != nil {
			panic(err)
		}
	})
	instance, err := Instantiate(compiled, InstantiateOptions{
		Imports: NewImports().Global("env", "base", GlobalImport{Type: ValI32, Bits: 7}).Function("env", "start", start),
		GC:      GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if starts != 1 {
		t.Fatalf("imported start calls=%d want=1", starts)
	}
	if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{42}) {
		t.Fatalf("run()=%v err=%v want=[42]", got, err)
	}
}

func TestRV64RootedScalarLocalStartWithImportExecutes(t *testing.T) {
	source := rootedScalarLocalStartWithImportModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("rooted local start with a numeric import was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || !view.HasStart || view.StartIsImport || view.StartLocalFunc != 0 || view.NumImports != 1 {
		t.Fatalf("rooted local start receipt=%t start=%t imported=%t local=%d imports=%d validation=%v", rv64FreshRootedGC(view), view.HasStart, view.StartIsImport, view.StartLocalFunc, view.NumImports, railshotValidateCompiled(view))
	}
	unused := slotHostFunc(func(HostModule, []uint64, []uint64) {
		panic("unused host import was called")
	})
	instance, err := Instantiate(compiled, InstantiateOptions{
		Imports: NewImports().Function("env", "unused", unused),
		GC:      GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{7}) {
		t.Fatalf("run()=%v err=%v want=[7]", got, err)
	}
}

func TestRV64RootedScalarNumericGlobalImportsExecute(t *testing.T) {
	source := rootedScalarNumericGlobalImportModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("rooted numeric global imports were not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || len(view.GlobalImports) != 4 || len(view.Globals) != 4 {
		t.Fatalf("rooted imported-global receipt=%t imports=%d globals=%d validation=%v", rv64FreshRootedGC(view), len(view.GlobalImports), len(view.Globals), railshotValidateCompiled(view))
	}
	for name, mutate := range map[string]func(*Compiled){
		"name": func(candidate *Compiled) {
			candidate.GlobalImports = slices.Clone(candidate.GlobalImports)
			candidate.GlobalImports[0].Name = "other"
		},
		"mutability": func(candidate *Compiled) {
			candidate.GlobalImports = slices.Clone(candidate.GlobalImports)
			candidate.GlobalImports[0].Mutable = false
		},
		"cell-type": func(candidate *Compiled) {
			candidate.Globals = slices.Clone(candidate.Globals)
			candidate.Globals[0].Type = ValI64
		},
		"count": func(candidate *Compiled) {
			candidate.GlobalImports = append(slices.Clone(candidate.GlobalImports), make([]GlobalImportDef, 253)...)
			candidate.Globals = append(slices.Clone(candidate.Globals), make([]GlobalDef, 253)...)
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated numeric global import retained the generic receipt")
			}
		})
	}
	i32 := NewGlobalI32(41, true)
	i64 := NewGlobalI64(-7, false)
	f32 := NewGlobalF32(math.Float32frombits(0x7f800001), false)
	f64 := NewGlobalF64(math.Float64frombits(0x7ff0000000000001), false)
	for _, global := range []*Global{i32, i64, f32, f64} {
		defer global.Close()
	}
	instance, err := Instantiate(compiled, InstantiateOptions{
		Imports: NewImports().
			Global("env", "i32", i32).
			Global("env", "i64", i64).
			Global("env", "f32", f32).
			Global("env", "f64", f64),
		GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	want := []uint64{42, I64(-7), 0x7f800001, 0x7ff0000000000001}
	if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, want) {
		t.Fatalf("run()=%x err=%v want=%x", got, err, want)
	}
	if got := i32.Get(); got != 42 {
		t.Fatalf("shared mutable i32=%d want=42", got)
	}
	if exported, err := instance.ExportedGlobalObject("cell"); err != nil || exported != i32 {
		t.Fatalf("imported global re-export lost identity: exported=%p want=%p err=%v", exported, i32, err)
	}
	dirty, err := Instantiate(compiled, InstantiateOptions{
		Imports: NewImports().
			Global("env", "i32", GlobalImport{Type: ValI32, Bits: 0xdeadbeef00000029, Mutable: true}).
			Global("env", "i64", GlobalImport{Type: ValI64, Bits: I64(-7)}).
			Global("env", "f32", GlobalImport{Type: ValF32, Bits: 0xfeedface7f800001}).
			Global("env", "f64", GlobalImport{Type: ValF64, Bits: 0x7ff0000000000001}),
		GC: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := dirty.Invoke("run"); err != nil || !slices.Equal(got, want) {
		_ = dirty.Close()
		t.Fatalf("dirty run()=%x err=%v want=%x", got, err, want)
	}
	if err := dirty.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRV64RootedScalarMixedFunctionGlobalImportsExecute(t *testing.T) {
	source := rootedScalarMixedFunctionGlobalImportModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("rooted mixed function/global imports were not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || view.NumImports != 1 || len(view.GlobalImports) != 1 || len(view.genericGCFrameRoots().callsites) != 1 {
		t.Fatalf("rooted mixed import receipt=%t funcs=%d globals=%d roots=%+v validation=%v", rv64FreshRootedGC(view), view.NumImports, len(view.GlobalImports), view.genericGCFrameRoots(), railshotValidateCompiled(view))
	}
	base := NewGlobalI32(1, false)
	defer base.Close()
	calls := 0
	host := slotHostFunc(func(module HostModule, params, results []uint64) {
		calls++
		gcModule, ok := module.(GCHostModule)
		if !ok {
			panic("rooted mixed import module has no collector surface")
		}
		if err := gcModule.CollectGC(); err != nil {
			panic(err)
		}
		results[0] = uint64(uint32(params[0]) + 1)
	})
	instance, err := Instantiate(compiled, InstantiateOptions{
		Imports: NewImports().Global("env", "base", base).Function("env", "host", host),
		GC:      GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for range 4 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{43}) {
			t.Fatalf("run()=%v err=%v want=[43]", got, err)
		}
	}
	if calls != 4 {
		t.Fatalf("host calls=%d want=4", calls)
	}
}

func TestRV64RootedScalarHostImportExecutes(t *testing.T) {
	source := rootedScalarHostImportModule()
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("bounded rooted numeric host import was not source-admitted")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	roots := view.genericGCFrameRoots()
	if !rv64FreshRootedGC(view) || view.NumImports != 1 || len(view.importFuncSigs) != 1 || roots == nil || len(roots.callsites) != 1 || len(roots.callsites[0].offsets) != 1 {
		t.Fatalf("rooted host-call receipt=%t imports=%d roots=%+v validation=%v", rv64FreshRootedGC(view), view.NumImports, roots, railshotValidateCompiled(view))
	}
	for name, mutate := range map[string]func(*Compiled){
		"name": func(candidate *Compiled) {
			candidate.Imports = slices.Clone(candidate.Imports)
			candidate.Imports[0] = "env.other.host.fn"
		},
		"separator": func(candidate *Compiled) {
			memo := *candidate.loadValidateMemo()
			memo.importModuleEnds = slices.Clone(memo.importModuleEnds)
			memo.importModuleEnds[0]++
			candidate.validateMemo = &memo
		},
		"signature": func(candidate *Compiled) {
			candidate.importFuncSigs = slices.Clone(candidate.importFuncSigs)
			candidate.importFuncSigs[0].Params = []ValType{ValI64}
		},
		"type-key": func(candidate *Compiled) {
			candidate.FuncTypeID = slices.Clone(candidate.FuncTypeID)
			candidate.FuncTypeID[0] ^= 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneCompiledMetadata(view)
			mutate(candidate)
			if rv64FreshRootedGC(candidate) {
				t.Fatal("mutated host import retained the generic receipt")
			}
			if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatalf("mutated host import validation=%v", err)
			}
		})
	}
	calls := 0
	host := slotHostFunc(func(module HostModule, params, results []uint64) {
		calls++
		runtime.GC()
		gcModule, ok := module.(GCHostModule)
		if !ok {
			panic("rooted host module has no collector surface")
		}
		if err := gcModule.CollectGC(); err != nil {
			panic(err)
		}
		results[0] = uint64(uint32(params[0]) + 1)
	})
	instance, err := Instantiate(compiled, InstantiateOptions{
		Imports: NewImports().Function("env.calls", "host.fn", host),
		GC:      GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for range 8 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{83}) {
			t.Fatalf("run()=%v err=%v want=[83]", got, err)
		}
	}
	if calls != 8 {
		t.Fatalf("host calls=%d want=8", calls)
	}
}

func TestRV64RootedScalarHostImportPrivateTableExecutes(t *testing.T) {
	source := rootedScalarHostImportPrivateTableModule(1)
	if !railshot.AdmitGCRootedScalarSource(source) {
		t.Fatal("bounded rooted host import with a private local-only table was not source-admitted")
	}
	if railshot.AdmitGCRootedScalarSource(rootedScalarHostImportPrivateTableModule(0)) {
		t.Fatal("table element exposing an imported function was source-admitted")
	}
	if railshot.AdmitGCRootedScalarSource(rootedScalarHostImportMixedTableModule()) {
		t.Fatal("public imported table caused a private local table to expose an imported function")
	}
	compiled, err := Compile(rv64RootedScalarConfig(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	view := compiled.executionView()
	if !rv64FreshRootedGC(view) || view.NumImports != 1 || view.tableCount() != 1 || len(view.Elems) != 1 || len(view.Elems[0].Values) != 1 || view.Elems[0].Values[0].FuncIndex != 1 {
		t.Fatalf("rooted host/private-table receipt=%t imports=%d tables=%d elements=%+v validation=%v", rv64FreshRootedGC(view), view.NumImports, view.tableCount(), view.Elems, railshotValidateCompiled(view))
	}
	candidate := cloneCompiledMetadata(view)
	candidate.Elems = slices.Clone(candidate.Elems)
	candidate.Elems[0].Values = slices.Clone(candidate.Elems[0].Values)
	candidate.Elems[0].Values[0].FuncIndex = 0
	if rv64FreshRootedGC(candidate) {
		t.Fatal("compiled table element exposing an imported function retained the generic receipt")
	}
	if err := railshotValidateCompiled(candidate); !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatalf("imported-function table element validation=%v", err)
	}
	calls := 0
	host := slotHostFunc(func(module HostModule, params, results []uint64) {
		calls++
		gcModule, ok := module.(GCHostModule)
		if !ok {
			panic("rooted host module has no collector surface")
		}
		if err := gcModule.CollectGC(); err != nil {
			panic(err)
		}
		results[0] = uint64(uint32(params[0]) + 1)
	})
	instance, err := Instantiate(compiled, InstantiateOptions{
		Imports: NewImports().Function("env.calls", "host.fn", host),
		GC:      GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for range 8 {
		if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, []uint64{125}) {
			t.Fatalf("run()=%v err=%v want=[125]", got, err)
		}
	}
	if calls != 8 {
		t.Fatalf("host calls=%d want=8", calls)
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
