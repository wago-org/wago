//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64PublicRootedLocalTailModule(vector bool) []byte {
	callee, caller, wrapper := []byte{
		0x00,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00,
	}, []byte{
		0x00,
		0x41, 0x2a, 0xfb, 0x00, 0x00,
	}, []byte{0x00}
	types := [][]byte{
		{0x5f, 0x01, 0x7f, 0x00},
		{0x60, 0x01, 0x64, 0x00, 0x01, 0x7f},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}
	if vector {
		types[1] = []byte{0x60, 0x02, 0x64, 0x00, 0x7b, 0x02, 0x7f, 0x7b}
		types[2] = wasmtest.FuncType([]wasm.ValType{wasm.V128}, []wasm.ValType{wasm.I32, wasm.V128})
		callee = append(callee, 0x20, 0x01)
		caller = append(caller, 0x20, 0x00)
		wrapper = append(wrapper, 0x20, 0x00)
	}
	callee = append(callee, 0x0b)
	caller = append(caller, 0x12, 0x00, 0x0b)
	wrapper = append(wrapper, 0x12, 0x01, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(2), wasmtest.ULEB(2))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 2))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(callee))), callee...),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
			append(wasmtest.ULEB(uint32(len(wrapper))), wrapper...),
		)),
	)
}

func TestRV64PublicRootedLocalTailExecution(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(map[bool]string{false: "scalar", true: "swar"}[vector], func(t *testing.T) {
			cfg := rv64I31Config().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTailCall)
			compiled, err := cfg.Compile(rv64PublicRootedLocalTailModule(vector))
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			roots := compiled.genericGCFrameRoots()
			tails := rv64NativeTailModule(compiled)
			if roots == nil || len(roots.safepoints) != 2 || len(roots.callsites) != 0 || tails == nil || len(tails.Plans) != 2 {
				t.Fatalf("rooted local tails: roots=%+v tails=%+v", roots, tails)
			}
			for _, gcConfig := range []GCConfig{
				{},
				{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true},
			} {
				rt := NewRuntime(WithRuntimeConfig(cfg))
				mod, err := rt.Module(compiled)
				if err != nil {
					rt.Close()
					t.Fatal(err)
				}
				in, err := rt.Instantiate(context.Background(), mod, WithGC(gcConfig))
				mod.Close()
				if err != nil {
					rt.Close()
					t.Fatal(err)
				}
				args, want := []uint64(nil), []uint64{42}
				if vector {
					args = []uint64{0x0123456789abcdef, 0xfedcba9876543210}
					want = append(want, args...)
				}
				for range 64 {
					if got, invokeErr := in.Invoke("run", args...); invokeErr != nil || !slices.Equal(got, want) {
						in.Close()
						rt.Close()
						t.Fatalf("rooted local tail = %v, %v; want %v", got, invokeErr, want)
					}
				}
				in.Close()
				rt.Close()
			}
		})
	}
}

func rv64PublicRootedImportedTailModule() []byte {
	imported := append(append(wasmtest.Name("env"), wasmtest.Name("tail")...), byte(wasm.ExternFunc), 0x01)
	withAllocation := []byte{
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		0x20, 0x00, 0x12, 0x00, 0x0b,
	}
	bridge := []byte{0x20, 0x00, 0x12, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(imported)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run_gc", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("run_tail", byte(wasm.ExternFunc), 2),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(withAllocation), wasmtest.Code(bridge))),
	)
}

func TestRV64PublicRootedImportedTailExecution(t *testing.T) {
	cfg := rv64I31Config().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTailCall)
	cfg.railshotState.tailProducts = true
	compiled, err := cfg.Compile(rv64PublicRootedImportedTailModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	roots := compiled.genericGCFrameRoots()
	tails := rv64NativeTailModule(compiled)
	if roots == nil || len(roots.safepoints) != 1 || len(roots.callsites) != 0 || tails == nil || len(tails.Plans) != 2 {
		t.Fatalf("rooted imported tails: roots=%+v tails=%+v", roots, tails)
	}
	for _, plan := range tails.Plans {
		if plan.Kind != encoder.NativeTailDirectImport || plan.ImportIndex != 0 {
			t.Fatalf("rooted imported tail plan: %+v", plan)
		}
	}
	for _, gcConfig := range []GCConfig{
		{},
		{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true},
	} {
		rt := NewRuntime(WithRuntimeConfig(cfg))
		mod, err := rt.Module(compiled)
		if err != nil {
			rt.Close()
			t.Fatal(err)
		}
		calls := 0
		host := HostCallFunc(func(call HostCall) {
			calls++
			if got := call.ParamSlots(); !slices.Equal(got, []uint64{0x80000001}) {
				panic("rooted imported tail i32 argument")
			}
			call.ResultSlots()[0] = 0xfeedface80000002
		})
		in, err := rt.Instantiate(context.Background(), mod,
			WithImports(NewImports().Function("env", "tail", host)), WithGC(gcConfig))
		mod.Close()
		if err != nil {
			rt.Close()
			t.Fatal(err)
		}
		for range 64 {
			for _, export := range []string{"run_gc", "run_tail"} {
				if got, invokeErr := in.Invoke(export, 0xcafe123480000001); invokeErr != nil || !slices.Equal(got, []uint64{0x80000002}) {
					in.Close()
					rt.Close()
					t.Fatalf("rooted imported tail %s = %v, %v", export, got, invokeErr)
				}
			}
		}
		if calls != 128 {
			in.Close()
			rt.Close()
			t.Fatalf("rooted imported tail calls = %d", calls)
		}
		in.Close()
		rt.Close()
	}
}

func rv64PublicRootedIndirectTailModule() []byte {
	target := []byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}
	withAllocation := []byte{
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		0x20, 0x00, 0x41, 0x00, 0x13, 0x01, 0x00, 0x0b,
	}
	bridge := []byte{0x20, 0x00, 0x41, 0x00, 0x13, 0x01, 0x00, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(4, []byte{0x01, 0x70, 0x01, 0x01, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run_gc", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("run_tail", byte(wasm.ExternFunc), 2),
		)),
		wasmtest.Section(9, []byte{0x01, 0x00, 0x41, 0x00, 0x0b, 0x01, 0x00}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(target), wasmtest.Code(withAllocation), wasmtest.Code(bridge))),
	)
}

func TestRV64PublicRootedIndirectTailExecution(t *testing.T) {
	cfg := rv64I31Config().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTailCall)
	cfg.railshotState.tailProducts = true
	compiled, err := cfg.Compile(rv64PublicRootedIndirectTailModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	roots := compiled.genericGCFrameRoots()
	tails := rv64NativeTailModule(compiled)
	if roots == nil || len(roots.safepoints) != 1 || len(roots.callsites) != 0 || tails == nil || len(tails.Plans) != 2 {
		t.Fatalf("rooted indirect tails: roots=%+v tails=%+v", roots, tails)
	}
	for _, plan := range tails.Plans {
		if plan.Kind != encoder.NativeTailIndirect || plan.TypeIndex != 1 {
			t.Fatalf("rooted indirect tail plan: %+v", plan)
		}
	}
	for _, gcConfig := range []GCConfig{
		{},
		{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true},
	} {
		rt := NewRuntime(WithRuntimeConfig(cfg))
		mod, err := rt.Module(compiled)
		if err != nil {
			rt.Close()
			t.Fatal(err)
		}
		in, err := rt.Instantiate(context.Background(), mod, WithGC(gcConfig))
		mod.Close()
		if err != nil {
			rt.Close()
			t.Fatal(err)
		}
		for range 64 {
			for _, export := range []string{"run_gc", "run_tail"} {
				if got, invokeErr := in.Invoke(export, 0xcafe123480000001); invokeErr != nil || !slices.Equal(got, []uint64{0x80000002}) {
					in.Close()
					rt.Close()
					t.Fatalf("rooted indirect tail %s = %v, %v", export, got, invokeErr)
				}
			}
		}
		in.Close()
		rt.Close()
	}
}

func rv64PublicRootedReferenceTailModule() []byte {
	target := []byte{0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b}
	withAllocation := []byte{
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x1a,
		0x20, 0x00, 0xd2, 0x00, 0x15, 0x01, 0x0b,
	}
	bridge := []byte{0x20, 0x00, 0xd2, 0x00, 0x15, 0x01, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run_gc", byte(wasm.ExternFunc), 1),
			wasmtest.ExportEntry("run_tail", byte(wasm.ExternFunc), 2),
		)),
		wasmtest.Section(9, []byte{0x01, 0x03, 0x00, 0x01, 0x00}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(target), wasmtest.Code(withAllocation), wasmtest.Code(bridge))),
	)
}

func TestRV64PublicRootedReferenceTailExecution(t *testing.T) {
	cfg := rv64I31Config().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTailCall | CoreFeatureTypedFunctionReferences)
	cfg.railshotState.tailProducts = true
	compiled, err := cfg.Compile(rv64PublicRootedReferenceTailModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	roots := compiled.genericGCFrameRoots()
	tails := rv64NativeTailModule(compiled)
	if roots == nil || len(roots.safepoints) != 1 || len(roots.callsites) != 0 || tails == nil || len(tails.Plans) != 2 {
		t.Fatalf("rooted reference tails: roots=%+v tails=%+v", roots, tails)
	}
	for _, plan := range tails.Plans {
		if plan.Kind != encoder.NativeTailReference || plan.TypeIndex != 1 || plan.ReferenceBase == 0 {
			t.Fatalf("rooted reference tail plan: %+v", plan)
		}
	}
	for _, gcConfig := range []GCConfig{
		{},
		{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true},
	} {
		rt := NewRuntime(WithRuntimeConfig(cfg))
		mod, err := rt.Module(compiled)
		if err != nil {
			rt.Close()
			t.Fatal(err)
		}
		in, err := rt.Instantiate(context.Background(), mod, WithGC(gcConfig))
		mod.Close()
		if err != nil {
			rt.Close()
			t.Fatal(err)
		}
		for range 64 {
			for _, export := range []string{"run_gc", "run_tail"} {
				if got, invokeErr := in.Invoke(export, 0xcafe123480000001); invokeErr != nil || !slices.Equal(got, []uint64{0x80000002}) {
					in.Close()
					rt.Close()
					t.Fatalf("rooted reference tail %s = %v, %v", export, got, invokeErr)
				}
			}
		}
		in.Close()
		rt.Close()
	}
}

func rv64PublicRootedReferenceCallModule() []byte {
	target := []byte{
		0x41, 0x09, 0xfb, 0x00, 0x00, 0x1a,
		0x20, 0x00, 0x41, 0x01, 0x6a, 0x0b,
	}
	caller := []byte{
		0x02, 0x01, 0x63, 0x00, 0x01, 0x7f,
		0x41, 0x07, 0xfb, 0x00, 0x00, 0x21, 0x01,
		0x20, 0x01,
		0x20, 0x00, 0xd2, 0x00, 0x14, 0x01,
		0x21, 0x02, 0xfb, 0x02, 0x00, 0x00, 0x20, 0x02, 0x6a, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 1))),
		wasmtest.Section(9, []byte{0x01, 0x03, 0x00, 0x01, 0x00}),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(target),
			append(wasmtest.ULEB(uint32(len(caller))), caller...),
		)),
	)
}

func TestRV64PublicRootedReferenceCallExecution(t *testing.T) {
	cfg := rv64I31Config().WithCoreFeatures(platformCoreFeatures() | CoreFeatureGC | CoreFeatureTypedFunctionReferences)
	cfg.railshotState.tailProducts = true
	compiled, err := cfg.Compile(rv64PublicRootedReferenceCallModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	roots := compiled.genericGCFrameRoots()
	if roots == nil || len(roots.safepoints) != 2 || len(roots.callsites) != 1 || len(roots.callsites[0].offsets) != 2 || roots.callsites[0].returnOffset == 0 || roots.callsites[0].stackAdjust == 0 || !compiled.nativeReferenceCallABI {
		t.Fatalf("rooted reference call: roots=%+v refcall=%t", roots, compiled.nativeReferenceCallABI)
	}
	for _, gcConfig := range []GCConfig{
		{},
		{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true},
	} {
		rt := NewRuntime(WithRuntimeConfig(cfg))
		mod, err := rt.Module(compiled)
		if err != nil {
			rt.Close()
			t.Fatal(err)
		}
		in, err := rt.Instantiate(context.Background(), mod, WithGC(gcConfig))
		mod.Close()
		if err != nil {
			rt.Close()
			t.Fatal(err)
		}
		for range 64 {
			if got, invokeErr := in.Invoke("run", 0xcafe123480000001); invokeErr != nil || !slices.Equal(got, []uint64{0x80000009}) {
				in.Close()
				rt.Close()
				t.Fatalf("rooted reference call = %v, %v", got, invokeErr)
			}
		}
		in.Close()
		rt.Close()
	}
}
