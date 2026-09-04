//go:build amd64

package dragline

import (
	"testing"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	runtimeabi "github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestAMD64PublishesDirectPreparedLeafAcrossCompilerPaths(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x07, 0x6a, 0x0b}))),
	)
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(module); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	input := corecompiler.Input{
		Module: module, Source: source, Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
		Target: target, Objective: corecompiler.ObjectiveSpeed, Bounds: corecompiler.BoundsSignals,
		ConfigurationFingerprint: [32]byte{1},
	}
	assertDirect := func(t *testing.T, output corecompiler.Output) {
		t.Helper()
		if len(output.DirectPrepared) == 0 || output.DirectPrepared[0]&1 == 0 {
			t.Fatal("AMD64 output omitted direct prepared metadata")
		}
		if len(output.DirectLeafPrepared) == 0 || output.DirectLeafPrepared[0]&1 == 0 {
			t.Fatal("AMD64 output omitted direct leaf metadata")
		}
		if len(output.ContextFreeLoopPrepared) != 0 && output.ContextFreeLoopPrepared[0]&1 != 0 {
			t.Fatal("AMD64 leaf redundantly published signal-guard-free call metadata")
		}
	}

	for _, workers := range []int{1, 2} {
		t.Run(map[int]string{1: "sequential", 2: "parallel"}[workers], func(t *testing.T) {
			input.FunctionWorkers = workers
			output, err := (Compiler{}).Compile(input)
			if err != nil {
				t.Fatal(err)
			}
			assertDirect(t, output)
		})
	}

	input.FunctionWorkers = 1
	cache := corecompiler.NewFunctionArtifactCache(1 << 20)
	compiler := Compiler{FunctionCache: cache}
	for _, name := range []string{"cold-cache", "warm-cache"} {
		t.Run(name, func(t *testing.T) {
			output, err := compiler.Compile(input)
			if err != nil {
				t.Fatal(err)
			}
			assertDirect(t, output)
		})
	}
}

func TestAMD64PublishesPreparedCallWithoutLeafMetadata(t *testing.T) {
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("tick")...)
	importEntry = append(importEntry, 0)
	importEntry = append(importEntry, wasmtest.ULEB(0)...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(importEntry)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0x00, 0x20, 0x00, 0x0b}))),
	)
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(module); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	input := corecompiler.Input{Module: module, Source: source, Target: target, ConfigurationFingerprint: [32]byte{2}}
	assertDirectCall := func(t *testing.T, output corecompiler.Output) {
		t.Helper()
		if len(output.DirectPrepared) == 0 || output.DirectPrepared[0]&1 == 0 {
			t.Fatal("AMD64 output omitted prepared-call metadata")
		}
		if len(output.DirectLeafPrepared) != 0 && output.DirectLeafPrepared[0]&1 != 0 {
			t.Fatal("AMD64 call-bearing function was published as a direct leaf")
		}
		if len(output.ContextFreeLoopPrepared) != 0 && output.ContextFreeLoopPrepared[0]&1 != 0 {
			t.Fatal("AMD64 imported-call function was published as signal-guard-free")
		}
	}
	for _, workers := range []int{1, 2} {
		input.FunctionWorkers = workers
		output, err := (Compiler{}).Compile(input)
		if err != nil {
			t.Fatal(err)
		}
		assertDirectCall(t, output)
	}
	input.FunctionWorkers = 1
	compiler := Compiler{FunctionCache: corecompiler.NewFunctionArtifactCache(1 << 20)}
	for range 2 {
		output, err := compiler.Compile(input)
		if err != nil {
			t.Fatal(err)
		}
		assertDirectCall(t, output)
	}
}

func TestAMD64PublishesTransitiveSignalGuardFreeClosure(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x10, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x07, 0x6a, 0x0b}),
		)),
	)
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(module); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	input := corecompiler.Input{Module: module, Source: source, Target: target, Bounds: corecompiler.BoundsSignals, ConfigurationFingerprint: [32]byte{3}}
	assertClosure := func(t *testing.T, output corecompiler.Output) {
		t.Helper()
		if len(output.ContextFreeLoopPrepared) == 0 || output.ContextFreeLoopPrepared[0]&0x3 != 0x1 {
			t.Fatalf("AMD64 signal-guard-free metadata = %x, want only call-bearing root", output.ContextFreeLoopPrepared)
		}
	}
	for _, workers := range []int{1, 2} {
		input.FunctionWorkers = workers
		output, err := (Compiler{}).Compile(input)
		if err != nil {
			t.Fatal(err)
		}
		assertClosure(t, output)
	}
	input.FunctionWorkers = 1
	compiler := Compiler{FunctionCache: corecompiler.NewFunctionArtifactCache(1 << 20)}
	for range 2 {
		output, err := compiler.Compile(input)
		if err != nil {
			t.Fatal(err)
		}
		assertClosure(t, output)
	}
}

func TestAMD64PublishesFoldedRotateBMI2Requirement(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x41, 0x07, 0x78, 0x0b}))),
	)
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(module); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	if !target.HasFeature(corecompiler.TargetFeatureAMD64BMI2) {
		t.Skip("host does not support BMI2")
	}
	input := corecompiler.Input{Module: module, Source: source, Target: target, ConfigurationFingerprint: [32]byte{4}}
	for _, workers := range []int{1, 2} {
		input.FunctionWorkers = workers
		output, err := (Compiler{}).Compile(input)
		if err != nil {
			t.Fatal(err)
		}
		if !output.RequiresBMI2 {
			t.Fatalf("workers %d: folded rotate did not publish BMI2 requirement", workers)
		}
	}
	input.FunctionWorkers = 1
	compiler := Compiler{FunctionCache: corecompiler.NewFunctionArtifactCache(1 << 20)}
	for pass := range 2 {
		output, err := compiler.Compile(input)
		if err != nil {
			t.Fatal(err)
		}
		if !output.RequiresBMI2 {
			t.Fatalf("cache pass %d: folded rotate lost BMI2 requirement", pass)
		}
	}
	target.FeatureBits[uint16(corecompiler.TargetFeatureAMD64BMI2)/64] &^= uint64(1) << (uint16(corecompiler.TargetFeatureAMD64BMI2) % 64)
	input.Target = target
	input.ConfigurationFingerprint = [32]byte{5}
	output, err := (Compiler{}).Compile(input)
	if err != nil {
		t.Fatal(err)
	}
	if output.RequiresBMI2 {
		t.Fatal("baseline target published a BMI2 requirement")
	}
}

func TestAMD64StructuredWritesSIMDBinaryDirectlyToTeeLocal(t *testing.T) {
	source := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x07, 0x01, 0x60, 0x02, 0x7b, 0x7b, 0x01, 0x7b,
		0x03, 0x02, 0x01, 0x00,
		0x0a, 0x12, 0x01, 0x10, 0x01, 0x01, 0x7b,
		0x20, 0x00, 0x20, 0x01, 0xfd, 0x51,
		0x20, 0x00, 0xfd, 0x51, 0x22, 0x02, 0x0b,
	}
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(module); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	var metrics Metrics
	if _, err := (Compiler{Metrics: &metrics}).Compile(corecompiler.Input{Module: module, Source: source, Target: target}); err != nil {
		t.Fatal(err)
	}
	if got := metrics.Functions[0].NativeBytes; got > 200 {
		t.Fatalf("direct SIMD tee emitted %d bytes, want at most 200", got)
	}
}
