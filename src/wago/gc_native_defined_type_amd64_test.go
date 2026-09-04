//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcNativeDefinedTypeModule() []byte {
	types := [][]byte{
		{0x50, 0x00, 0x5f, 0x01, 0x7f, 0x01},                   // open struct 0
		{0x4f, 0x01, 0x00, 0x5f, 0x01, 0x7f, 0x01},             // final struct 1 <: 0
		{0x4f, 0x01, 0x00, 0x5f, 0x02, 0x7f, 0x01, 0x7f, 0x01}, // distinct final sibling struct 2 <: 0
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	global := []byte{0x6d, 0x01, 0x41, 0x07, 0xfb, 0x00, 0x01, 0x0b} // mutable eqref = struct.new 1
	bodies := [][]byte{
		wasmtest.Code([]byte{0x23, 0x00, 0xfb, 0x16, 0x00, 0x1a, 0x41, 0x01, 0x0b}),       // cast super
		wasmtest.Code([]byte{0x23, 0x00, 0xfb, 0x14, 0x00, 0x0b}),                         // test super
		wasmtest.Code([]byte{0x23, 0x00, 0xfb, 0x14, 0x02, 0x0b}),                         // test sibling
		wasmtest.Code([]byte{0x23, 0x00, 0xfb, 0x16, 0x02, 0x1a, 0x41, 0x01, 0x0b}),       // cast sibling
		wasmtest.Code([]byte{0xd0, 0x6e, 0xfb, 0x17, 0x00, 0x1a, 0x41, 0x01, 0x0b}),       // cast_null null
		wasmtest.Code([]byte{0xd0, 0x6e, 0xfb, 0x16, 0x00, 0x1a, 0x41, 0x01, 0x0b}),       // cast null
		wasmtest.Code([]byte{0xd0, 0x6e, 0xfb, 0x14, 0x00, 0x0b}),                         // test null
		wasmtest.Code([]byte{0xd0, 0x6e, 0xfb, 0x15, 0x00, 0x0b}),                         // test_null null
		wasmtest.Code([]byte{0x23, 0x00, 0xfb, 0x16, 0x62, 0x00, 0x1a, 0x41, 0x01, 0x0b}), // exact cast to open super
		wasmtest.Code([]byte{0x23, 0x00, 0xfb, 0x16, 0x00, 0xfb, 0x14, 0x01, 0x0b}),       // cast open super, then test original child
	}
	funcs := make([][]byte, len(bodies))
	exports := make([][]byte, len(bodies))
	names := []string{"cast_super", "test_super", "test_sibling", "cast_sibling", "cast_null", "cast_nonnull_null", "test_null", "test_null_nullable", "cast_exact_super", "cast_super_then_test_child"}
	for i := range bodies {
		funcs[i] = wasmtest.ULEB(3)
		exports[i] = wasmtest.ExportEntry(names[i], byte(wasm.ExternFunc), uint32(i))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func assertGCNativeDefinedTypeSemantics(t *testing.T, in *Instance) {
	t.Helper()
	for _, tc := range []struct {
		name string
		want uint64
	}{
		{"cast_super", 1}, {"test_super", 1}, {"test_sibling", 0},
		{"cast_null", 1}, {"test_null", 0}, {"test_null_nullable", 1},
		{"cast_super_then_test_child", 1},
	} {
		got, err := in.Invoke(tc.name)
		if err != nil || len(got) != 1 || got[0] != tc.want {
			t.Fatalf("%s = %v, %v; want [%d]", tc.name, got, err, tc.want)
		}
	}
	for _, name := range []string{"cast_sibling", "cast_nonnull_null", "cast_exact_super"} {
		_, err := in.Invoke(name)
		var trap *TrapError
		if !errors.As(err, &trap) || (trap.Code != TrapCastFailure && trap.Code != TrapNullReference) {
			t.Fatalf("%s = %v; want cast/null trap", name, err)
		}
	}
}

func TestGCNativeDefinedCastAndTestAcrossCollectorSpaces(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeDefinedTypeModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()

	throughput, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	assertGCNativeDefinedTypeSemantics(t, throughput) // nursery
	object := corergc.Ref(uint32(readGlobalObject(throughput.globalCells[0], throughput.c.Globals[0].Type)))
	if err := throughput.gc.ForcePromote(object); err != nil {
		t.Fatal(err)
	}
	assertGCNativeDefinedTypeSemantics(t, throughput) // old/moved
	if err := throughput.gc.CollectFull(nil); err != nil {
		t.Fatal(err)
	}
	assertGCNativeDefinedTypeSemantics(t, throughput)
	if err := throughput.Close(); err != nil {
		t.Fatal(err)
	}

	tiny, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true, StressBarriers: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer tiny.Close()
	for range 50 {
		assertGCNativeDefinedTypeSemantics(t, tiny)
	}
}

func TestGCNativeSubtypeIntervalAppendWaitsForInvocationLease(t *testing.T) {
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
	defer rt.Close()
	baseType := []byte{0x50, 0x00, 0x5f, 0x00}
	baseGlobal := []byte{0x6d, 0x01, 0xfb, 0x01, 0x00, 0x0b}
	base, err := rt.Compile(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(baseType)),
		wasmtest.Section(6, wasmtest.Vec(baseGlobal)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", byte(wasm.ExternGlobal), 0))),
	))
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	baseInstance, err := rt.Instantiate(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	defer baseInstance.Close()
	sharedGlobal, err := baseInstance.ExportedGlobalObject("g")
	if err != nil {
		t.Fatal(err)
	}

	childType := []byte{0x4f, 0x01, 0x00, 0x5f, 0x00}
	consumerImport := append(append(wasmtest.Name("env"), wasmtest.Name("g")...), byte(wasm.ExternGlobal), 0x6d, 0x01)
	consumer, err := rt.Compile(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(baseType, childType)),
		wasmtest.Section(2, wasmtest.Vec(consumerImport)),
	))
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()

	domain := baseInstance.gcInvocationDomain()
	if domain == nil || domain.collector.NativeView().SubtypeIntervalCount != 1 {
		t.Fatalf("initial GC domain/view = %+v/%+v", domain, baseInstance.gc.NativeView())
	}
	type result struct {
		instance *Instance
		err      error
	}
	done := make(chan result, 1)
	domain.invocationMu.Lock()
	locked := true
	defer func() {
		if locked {
			domain.invocationMu.Unlock()
		}
	}()
	go func() {
		instance, err := rt.Instantiate(context.Background(), consumer, WithImports(Imports{"env.g": sharedGlobal}))
		done <- result{instance: instance, err: err}
	}()
	select {
	case got := <-done:
		if got.instance != nil {
			_ = got.instance.Close()
		}
		t.Fatalf("type append completed while native invocation lease was held: %v", got.err)
	case <-time.After(50 * time.Millisecond):
	}
	if got := domain.collector.NativeView().SubtypeIntervalCount; got != 1 {
		t.Fatalf("blocked type append published %d subtype intervals, want 1", got)
	}
	domain.invocationMu.Unlock()
	locked = false
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		defer got.instance.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("type append did not resume after native invocation lease release")
	}
	if got := domain.collector.NativeView().SubtypeIntervalCount; got != 2 {
		t.Fatalf("completed type append published %d subtype intervals, want 2", got)
	}

	viewDone := make(chan error, 1)
	domain.invocationMu.Lock()
	locked = true
	go func() {
		builder := instanceBuilder{collector: domain.collector, gcDomain: domain}
		_, err := builder.buildNativeGCInstanceView([]corergc.TypeID{0, 1})
		viewDone <- err
	}()
	select {
	case err := <-viewDone:
		t.Fatalf("native view validation completed while invocation lease was held: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	domain.invocationMu.Unlock()
	locked = false
	select {
	case err := <-viewDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("native view validation did not resume after invocation lease release")
	}
}

func gcNativeLargeArraySubtypeModule() []byte {
	types := [][]byte{
		{0x50, 0x00, 0x5e, 0x7f, 0x01},       // open array 0
		{0x4f, 0x01, 0x00, 0x5e, 0x7f, 0x01}, // final array 1 <: 0
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	global := []byte{0x6d, 0x01, 0x41}
	global = append(global, wasmtest.SLEB32(10000)...)
	global = append(global, 0xfb, 0x07, 0x01, 0x0b)
	body := []byte{0x23, 0x00, 0xfb, 0x14, 0x00, 0x23, 0x00, 0xfb, 0x16, 0x00, 0x1a, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestGCNativeDefinedTypeChecksLargeObjects(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeLargeArraySubtypeModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{ThroughputHeapBytes: 2 << 20, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	got, err := instance.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != 1 {
		t.Fatalf("large-array subtype run = %v, %v; want [1]", got, err)
	}
}

func gcNativeDeepSubtypeModule(depth int, linked bool) []byte {
	types := make([][]byte, 0, depth+1)
	types = append(types, []byte{0x50, 0x00, 0x5f, 0x00})
	for i := 1; i < depth; i++ {
		prefix := byte(0x50)
		if i == depth-1 {
			prefix = 0x4f
		}
		entry := []byte{prefix, 0x01}
		entry = append(entry, wasmtest.ULEB(uint32(i-1))...)
		entry = append(entry, 0x5f, 0x00)
		types = append(types, entry)
	}
	types = append(types, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))
	global := []byte{0x6d, 0x01, 0xfb, 0x01}
	global = append(global, wasmtest.ULEB(uint32(depth-1))...)
	global = append(global, 0x0b)
	globalIndex := byte(0)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...))}
	if linked {
		globalIndex = 1
		imp := append(append(wasmtest.Name("env"), wasmtest.Name("g")...), byte(wasm.ExternGlobal), 0x6d, 0x01)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(imp)))
	}
	body := []byte{0x23, globalIndex, 0xfb, 0x14, 0x00, 0x23, globalIndex, 0xfb, 0x16, 0x00, 0x1a, 0x0b}
	sections = append(sections,
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(uint32(depth)))),
		wasmtest.Section(6, wasmtest.Vec(global)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	return wasmtest.Module(sections...)
}

func TestGCNativeDefinedTypeDeepSubtypeAndCanonicalMap(t *testing.T) {
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
	defer rt.Close()
	paddingGlobal := []byte{0x6d, 0x01, 0x41, 0x01, 0xfb, 0x07, 0x00, 0x0b}
	padding, err := rt.Compile(wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 0x01})),
		wasmtest.Section(6, wasmtest.Vec(paddingGlobal)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", byte(wasm.ExternGlobal), 0))),
	))
	if err != nil {
		t.Fatal(err)
	}
	defer padding.Close()
	paddingInstance, err := rt.Instantiate(context.Background(), padding)
	if err != nil {
		t.Fatal(err)
	}
	defer paddingInstance.Close()

	sharedGlobal, err := paddingInstance.ExportedGlobalObject("g")
	if err != nil {
		t.Fatal(err)
	}
	module, err := rt.Compile(gcNativeDeepSubtypeModule(32, true))
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	instance, err := rt.Instantiate(context.Background(), module, WithImports(Imports{"env.g": sharedGlobal}))
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if target, ok := instance.gcDomainType(0); !ok || target == 0 {
		t.Fatalf("module-local type 0 canonicalized to %d, %v; want shifted Runtime-domain identity", target, ok)
	}
	got, err := instance.Invoke("run")
	if err != nil || len(got) != 1 || got[0] != 1 {
		t.Fatalf("deep subtype run = %v, %v; want [1]", got, err)
	}
}
