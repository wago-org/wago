package dragline

import (
	"bytes"
	"runtime"
	"slices"
	"testing"

	corecompiler "github.com/wago-org/wago/src/core/compiler"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	runtimeabi "github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestCompilerReusesRelocatableFunctionArtifacts(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 7, 0x41, 1, 0x6a, 0x0b}),
			wasmtest.Code([]byte{0x10, 0, 0x0b}),
		)),
	)
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(module); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetCompatibility)
	if err != nil {
		t.Fatal(err)
	}
	input := corecompiler.Input{
		Module: module, Source: source, Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
		Target: target, Objective: corecompiler.ObjectiveSpeed, Bounds: corecompiler.BoundsExplicit,
		ConfigurationFingerprint: [32]byte{1},
	}
	cache := corecompiler.NewFunctionArtifactCache(1 << 20)
	var metrics Metrics
	compiler := Compiler{Metrics: &metrics, FunctionCache: cache}
	first, err := compiler.Compile(input)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Functions[0].CacheHit || metrics.Functions[1].CacheHit {
		t.Fatalf("cold compile reported cache hits: %#v", metrics.Functions)
	}
	second, err := compiler.Compile(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Code, second.Code) || !equalInts(first.Entry, second.Entry) || !equalInts(first.InternalEntry, second.InternalEntry) {
		t.Fatal("warm function artifacts changed finalized module output")
	}
	if len(metrics.Functions) != 2 || !metrics.Functions[0].CacheHit || !metrics.Functions[1].CacheHit {
		t.Fatalf("warm compile metrics = %#v", metrics.Functions)
	}
	stats := cache.Stats()
	if stats.Entries != 2 || stats.Hits != 2 || stats.Misses != 2 {
		t.Fatalf("function cache stats = %#v on %s", stats, runtime.GOARCH)
	}
	dependencies, ok := functionArtifactDependencies(input, module, cache)
	if !ok {
		t.Fatal("compiled module no longer has cache dependencies")
	}
	for localIndex := range module.Code {
		identity, err := functionArtifactIdentity(input, module, localIndex, dependencies)
		if err != nil {
			t.Fatal(err)
		}
		artifact, hit, err := cache.Get(identity)
		if err != nil || !hit {
			t.Fatalf("cached function %d = hit %t, err %v", localIndex, hit, err)
		}
		if len(artifact.Sources) < 2 || artifact.Sources[0].NativeOffset != artifact.PrivateEntry {
			t.Fatalf("cached function %d source mappings = %#v, private entry %d", localIndex, artifact.Sources, artifact.PrivateEntry)
		}
		if err := artifact.Validate(); err != nil {
			t.Fatalf("cached function %d artifact validation: %v", localIndex, err)
		}
		if localIndex == 0 {
			distinct := false
			for _, source := range artifact.Sources[1:] {
				distinct = distinct || source.WasmOffset != artifact.Sources[0].WasmOffset
			}
			if !distinct {
				t.Fatalf("cached function %d has no instruction-granular Wasm source: %#v", localIndex, artifact.Sources)
			}
		} else if len(artifact.Safepoints) != 1 || artifact.Safepoints[0].RootCount != 0 || int(artifact.Safepoints[0].Offset) >= len(artifact.Code) {
			t.Fatalf("cached caller safepoints = %#v", artifact.Safepoints)
		}
	}
}

func TestCompilerReusesHelperSafepointArtifacts(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xfb, 0x01, 0x00, 0x1a, 0x10, 0x01, 0x0b}),
			wasmtest.Code([]byte{0xfb, 0x01, 0x00, 0xd1, 0x0b}),
		)),
	)
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(module); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetCompatibility)
	if err != nil {
		t.Fatal(err)
	}
	input := corecompiler.Input{
		Module: module, Source: source, Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
		Target: target, Objective: corecompiler.ObjectiveSpeed, Bounds: corecompiler.BoundsExplicit,
		ConfigurationFingerprint: [32]byte{11},
	}
	cache := corecompiler.NewFunctionArtifactCache(1 << 20)
	var metrics Metrics
	compiler := Compiler{Metrics: &metrics, FunctionCache: cache}
	first, err := compiler.Compile(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.GCSafepoints) != 2 || first.GCSafepoints[0].ID != 1 || first.GCSafepoints[1].ID != 2 {
		t.Fatalf("cold helper safepoints = %#v", first.GCSafepoints)
	}
	second, err := compiler.Compile(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Code, second.Code) || !slices.Equal(first.GCSafepoints, second.GCSafepoints) {
		t.Fatal("warm helper artifacts changed code or safepoint identity")
	}
	if len(metrics.Functions) != 2 || !metrics.Functions[0].CacheHit || !metrics.Functions[1].CacheHit {
		t.Fatalf("warm helper metrics = %#v", metrics.Functions)
	}
	stats := cache.Stats()
	if stats.Entries != 2 || stats.Hits != 2 || stats.Misses != 2 {
		t.Fatalf("helper function cache stats = %#v", stats)
	}
}

func TestHelperSafepointBaseInvalidatesArtifactIdentity(t *testing.T) {
	moduleSource := func(firstBody []byte) []byte {
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(
				[]byte{0x5f, 0x00},
				wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			)),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1))),
			wasmtest.Section(10, wasmtest.Vec(
				wasmtest.Code(firstBody),
				wasmtest.Code([]byte{0xfb, 0x01, 0x00, 0xd1, 0x0b}),
			)),
		)
	}
	compileInput := func(source []byte) corecompiler.Input {
		module, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(module); err != nil {
			t.Fatal(err)
		}
		target, err := corecompiler.HostTarget(corecompiler.TargetCompatibility)
		if err != nil {
			t.Fatal(err)
		}
		return corecompiler.Input{
			Module: module, Source: source, Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
			Target: target, Objective: corecompiler.ObjectiveSpeed, Bounds: corecompiler.BoundsExplicit,
			ConfigurationFingerprint: [32]byte{12},
		}
	}
	first := compileInput(moduleSource([]byte{0xfb, 0x01, 0x00, 0xd1, 0x0b}))
	second := compileInput(moduleSource([]byte{0xfb, 0x01, 0x00, 0x1a, 0xfb, 0x01, 0x00, 0xd1, 0x0b}))
	cache := corecompiler.NewFunctionArtifactCache(1 << 20)
	var metrics Metrics
	compiler := Compiler{Metrics: &metrics, FunctionCache: cache}
	if _, err := compiler.Compile(first); err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.Compile(second); err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 2 || metrics.Functions[0].CacheHit || metrics.Functions[1].CacheHit {
		t.Fatalf("shifted helper-base metrics = %#v; both artifacts must miss", metrics.Functions)
	}
}

func TestFunctionArtifactCapturesRailMachSourcesAndTraps(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x28, 0x02, 0x00, 0x0b}))),
	)
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(module); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetCompatibility)
	if err != nil {
		t.Fatal(err)
	}
	input := corecompiler.Input{
		Module: module, Source: source, Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
		Target: target, Objective: corecompiler.ObjectiveSpeed, Bounds: corecompiler.BoundsExplicit,
		ConfigurationFingerprint: [32]byte{2},
	}
	cache := corecompiler.NewFunctionArtifactCache(1 << 20)
	compiler := Compiler{FunctionCache: cache}
	if _, err := compiler.Compile(input); err != nil {
		t.Fatal(err)
	}
	dependencies, ok := functionArtifactDependencies(input, module, cache)
	if !ok {
		t.Fatal("compiled memory module has no cache dependencies")
	}
	identity, err := functionArtifactIdentity(input, module, 0, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	artifact, hit, err := cache.Get(identity)
	if err != nil || !hit {
		t.Fatalf("cached memory function = hit %t, err %v", hit, err)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(artifact.Sources) < 2 || artifact.Sources[0].NativeOffset != artifact.PrivateEntry {
		t.Fatalf("memory function sources = %#v, private entry %d", artifact.Sources, artifact.PrivateEntry)
	}
	if len(artifact.Traps) != 1 || artifact.Traps[0].Code != 3 || artifact.Traps[0].WasmOffset != 2 {
		t.Fatalf("memory function traps = %#v, want bounds trap at Wasm offset 2", artifact.Traps)
	}
}

func TestFunctionArtifactCapturesAllConversionTrapStubs(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.F32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0xa8, 0x0b}))),
	)
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(module); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetCompatibility)
	if err != nil {
		t.Fatal(err)
	}
	input := corecompiler.Input{
		Module: module, Source: source, Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
		Target: target, Objective: corecompiler.ObjectiveSpeed, Bounds: corecompiler.BoundsExplicit,
		ConfigurationFingerprint: [32]byte{3},
	}
	cache := corecompiler.NewFunctionArtifactCache(1 << 20)
	if _, err := (Compiler{FunctionCache: cache}).Compile(input); err != nil {
		t.Fatal(err)
	}
	dependencies, ok := functionArtifactDependencies(input, module, cache)
	if !ok {
		t.Fatal("compiled conversion module has no cache dependencies")
	}
	identity, err := functionArtifactIdentity(input, module, 0, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	artifact, hit, err := cache.Get(identity)
	if err != nil || !hit {
		t.Fatalf("cached conversion function = hit %t, err %v", hit, err)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(artifact.Traps) != 3 {
		t.Fatalf("conversion trap stubs = %#v, want three ordered checks", artifact.Traps)
	}
	for _, trap := range artifact.Traps {
		if trap.Code != 11 || trap.WasmOffset != 2 {
			t.Fatalf("conversion trap stub = %#v, want code 11 at Wasm offset 2", trap)
		}
	}
}

func TestFunctionArtifactCapturesStructuredStackSources(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x02, 0x40, // block
			0x41, 0x01, // i32.const 1
			0x1a,       // drop
			0x0b,       // end block
			0x41, 0x07, // i32.const 7
			0x0b, // end function
		}))),
	)
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(module); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetCompatibility)
	if err != nil {
		t.Fatal(err)
	}
	input := corecompiler.Input{
		Module: module, Source: source, Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
		Target: target, Objective: corecompiler.ObjectiveSpeed, Bounds: corecompiler.BoundsExplicit,
		ConfigurationFingerprint: [32]byte{4},
	}
	cache := corecompiler.NewFunctionArtifactCache(1 << 20)
	if _, err := (Compiler{FunctionCache: cache}).Compile(input); err != nil {
		t.Fatal(err)
	}
	dependencies, ok := functionArtifactDependencies(input, module, cache)
	if !ok {
		t.Fatal("compiled structured module has no cache dependencies")
	}
	identity, err := functionArtifactIdentity(input, module, 0, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	artifact, hit, err := cache.Get(identity)
	if err != nil || !hit {
		t.Fatalf("cached structured function = hit %t, err %v", hit, err)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(artifact.Sources) < 2 || artifact.Sources[0].NativeOffset != artifact.PrivateEntry {
		t.Fatalf("structured function sources = %#v, private entry %d", artifact.Sources, artifact.PrivateEntry)
	}
	distinct := false
	for _, source := range artifact.Sources[1:] {
		if source.WasmOffset != artifact.Sources[0].WasmOffset {
			distinct = true
			break
		}
	}
	if !distinct {
		t.Fatalf("structured function has no instruction-granular Wasm source: %#v", artifact.Sources)
	}
}

func TestFunctionArtifactCapturesStructuredStackTrap(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{
			0x02, 0x7f, // block (result i32)
			0x20, 0x00, // local.get 0
			0x28, 0x02, 0x00, // i32.load
			0x0b, // end block
			0x0b, // end function
		}))),
	)
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(module); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetCompatibility)
	if err != nil {
		t.Fatal(err)
	}
	input := corecompiler.Input{
		Module: module, Source: source, Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
		Target: target, Objective: corecompiler.ObjectiveSpeed, Bounds: corecompiler.BoundsExplicit,
		ConfigurationFingerprint: [32]byte{5},
	}
	cache := corecompiler.NewFunctionArtifactCache(1 << 20)
	if _, err := (Compiler{FunctionCache: cache}).Compile(input); err != nil {
		t.Fatal(err)
	}
	dependencies, ok := functionArtifactDependencies(input, module, cache)
	if !ok {
		t.Fatal("compiled structured memory module has no cache dependencies")
	}
	identity, err := functionArtifactIdentity(input, module, 0, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	artifact, hit, err := cache.Get(identity)
	if err != nil || !hit {
		t.Fatalf("cached structured memory function = hit %t, err %v", hit, err)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(artifact.Traps) != 1 || artifact.Traps[0].Code != 3 || artifact.Traps[0].WasmOffset != 4 {
		t.Fatalf("structured memory traps = %#v, want bounds trap at Wasm offset 4", artifact.Traps)
	}
}

func TestFunctionArtifactCapturesStructuredStackSafepoint(t *testing.T) {
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x07, 0x0b}),
			wasmtest.Code([]byte{
				0x02, 0x7f, // block (result i32)
				0x10, 0x00, // call 0
				0x0b, // end block
				0x0b, // end function
			}),
		)),
	)
	module, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(module); err != nil {
		t.Fatal(err)
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetCompatibility)
	if err != nil {
		t.Fatal(err)
	}
	input := corecompiler.Input{
		Module: module, Source: source, Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
		Target: target, Objective: corecompiler.ObjectiveSpeed, Bounds: corecompiler.BoundsExplicit,
		ConfigurationFingerprint: [32]byte{6},
	}
	cache := corecompiler.NewFunctionArtifactCache(1 << 20)
	if _, err := (Compiler{FunctionCache: cache}).Compile(input); err != nil {
		t.Fatal(err)
	}
	dependencies, ok := functionArtifactDependencies(input, module, cache)
	if !ok {
		t.Fatal("compiled structured caller has no cache dependencies")
	}
	identity, err := functionArtifactIdentity(input, module, 1, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	artifact, hit, err := cache.Get(identity)
	if err != nil || !hit {
		t.Fatalf("cached structured caller = hit %t, err %v", hit, err)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(artifact.Relocations) != 1 || len(artifact.Safepoints) != 1 || artifact.Safepoints[0].RootCount != 0 || int(artifact.Safepoints[0].Offset) >= len(artifact.Code) || artifact.AdapterReturnOffset == 0 || int(artifact.AdapterReturnOffset) >= len(artifact.Code) {
		t.Fatalf("structured caller relocations = %#v, safepoints = %#v", artifact.Relocations, artifact.Safepoints)
	}
}

func TestFunctionArtifactCapturesRailMachCollectorRoots(t *testing.T) {
	importEntry := append(wasmtest.Name("env"), wasmtest.Name("tick")...)
	importEntry = append(importEntry, 0)
	importEntry = append(importEntry, wasmtest.ULEB(0)...)
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.AnyRef}),
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
	target, err := corecompiler.HostTarget(corecompiler.TargetCompatibility)
	if err != nil {
		t.Fatal(err)
	}
	input := corecompiler.Input{
		Module: module, Source: source, Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
		Target: target, Objective: corecompiler.ObjectiveSpeed, Bounds: corecompiler.BoundsExplicit,
		ConfigurationFingerprint: [32]byte{7},
	}
	cache := corecompiler.NewFunctionArtifactCache(1 << 20)
	output, err := (Compiler{FunctionCache: cache}).Compile(input)
	if err != nil {
		t.Fatal(err)
	}
	wantAdjust := uint32(32)
	if runtime.GOARCH == "arm64" {
		wantAdjust = 48
	}
	if len(output.GCCallsites) != 1 || len(output.GCRoots) != 1 || output.GCCallsites[0].RootCount != 1 || output.GCCallsites[0].StackAdjust != wantAdjust || output.GCCallsites[0].FrameBytes == 0 || output.GCRoots[0] >= output.GCCallsites[0].FrameBytes {
		t.Fatalf("runtime GC callsites=%#v roots=%#v", output.GCCallsites, output.GCRoots)
	}
	dependencies, ok := functionArtifactDependencies(input, module, cache)
	if !ok {
		t.Fatal("compiled collector-root function has no cache dependencies")
	}
	identity, err := functionArtifactIdentity(input, module, 0, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	artifact, hit, err := cache.Get(identity)
	if err != nil || !hit {
		t.Fatalf("cached collector-root function = hit %t, err %v", hit, err)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(artifact.Safepoints) != 1 || artifact.Safepoints[0].RootCount != 1 || len(artifact.Roots) != 1 || artifact.Roots[0].Kind != corecompiler.RootLocationStack || artifact.Roots[0].Bank != corecompiler.RootBankCollector {
		t.Fatalf("collector-root artifact safepoints=%#v roots=%#v", artifact.Safepoints, artifact.Roots)
	}
}

func TestFunctionArtifactHostEffectsInvalidateOnlyConsumers(t *testing.T) {
	importEntry := func(name string) []byte {
		entry := append(wasmtest.Name("env"), wasmtest.Name(name)...)
		entry = append(entry, 0)
		return append(entry, wasmtest.ULEB(0)...)
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("left"), importEntry("right"))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 0, 0x0b}),
			wasmtest.Code([]byte{0x10, 1, 0x0b}),
		)),
	)
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	cache := corecompiler.NewFunctionArtifactCache(1)
	input := corecompiler.Input{
		Module: m, Source: source, Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
		ConfigurationFingerprint: [32]byte{1},
		HostEffects: []corecompiler.HostEffectBinding{
			{Declared: true, Contract: corecompiler.HostEffectContract{Reads: corecompiler.HostHeapGlobal}},
			{Declared: true, Contract: corecompiler.HostEffectContract{Reads: corecompiler.HostHeapTable}},
		},
	}
	before, ok := functionArtifactDependencies(input, m, cache)
	if !ok {
		t.Fatal("host effect dependencies were not cacheable")
	}
	input.HostEffects[0].Contract.Reads = corecompiler.HostHeapLinearMemory
	after, ok := functionArtifactDependencies(input, m, cache)
	if !ok {
		t.Fatal("changed host effect dependencies were not cacheable")
	}
	if before.specialization[0] == after.specialization[0] {
		t.Fatal("consumer cache identity ignored its changed host contract")
	}
	if before.specialization[1] != after.specialization[1] {
		t.Fatal("unrelated function cache identity consumed another import contract")
	}
}

func TestInitialNativeCodeCapacityBoundsSpeculation(t *testing.T) {
	module := &wasm.Module{Code: []wasm.Func{{BodyBytes: make([]byte, 100)}, {BodyBytes: make([]byte, 200)}}}
	if got, want := initialNativeCodeCapacity(module), 2*64+300; got != want {
		t.Fatalf("small-module capacity = %d, want %d", got, want)
	}
	code := make([]byte, 7, initialNativeCodeCapacity(module))
	code = growNativeCodeFromObservation(code, module, 100, 400)
	if got, want := cap(code), 2*64+300; got != want || len(code) != 7 {
		t.Fatalf("minor observed expansion capacity = %d/%d, want unchanged %d/7", got, len(code), want)
	}
	module.Code[1].BodyBytes = make([]byte, 9900)
	code = make([]byte, 7, initialNativeCodeCapacity(module))
	code = growNativeCodeFromObservation(code, module, 100, 400)
	if got, want := cap(code), 2*64+10000+30000; got != want || len(code) != 7 {
		t.Fatalf("material observed expansion capacity = %d/%d, want %d/7", got, len(code), want)
	}
	module.Code = []wasm.Func{{BodyBytes: make([]byte, 128<<10)}}
	code = growNativeCodeFromObservation(nil, module, 1, 1<<20)
	if got, want := cap(code), 64+(128<<10)+(192<<10); got != want {
		t.Fatalf("observed large-module capacity = %d, want bounded %d", got, want)
	}
}

func TestCompilerFunctionCachePreservesUnrelatedBodyHits(t *testing.T) {
	moduleSource := func(last byte) []byte {
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
			wasmtest.Section(10, wasmtest.Vec(
				wasmtest.Code([]byte{0x41, 7, 0x0b}),
				wasmtest.Code([]byte{0x10, 0, 0x0b}),
				wasmtest.Code([]byte{0x41, last, 0x0b}),
			)),
		)
	}
	decode := func(source []byte) *wasm.Module {
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetCompatibility)
	if err != nil {
		t.Fatal(err)
	}
	input := func(source []byte) corecompiler.Input {
		return corecompiler.Input{
			Module: decode(source), Source: source, Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
			Target: target, Objective: corecompiler.ObjectiveSpeed, Bounds: corecompiler.BoundsExplicit,
			ConfigurationFingerprint: [32]byte{1},
		}
	}
	cache := corecompiler.NewFunctionArtifactCache(1 << 20)
	var metrics Metrics
	compiler := Compiler{Metrics: &metrics, FunctionCache: cache}
	if _, err := compiler.Compile(input(moduleSource(9))); err != nil {
		t.Fatal(err)
	}
	changedInput := input(moduleSource(10))
	got, err := compiler.Compile(changedInput)
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 3 || !metrics.Functions[0].CacheHit || !metrics.Functions[1].CacheHit || metrics.Functions[2].CacheHit {
		t.Fatalf("partial-hit metrics = %#v", metrics.Functions)
	}
	want, err := (Compiler{}).Compile(changedInput)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Code, want.Code) || !equalInts(got.Entry, want.Entry) || !equalInts(got.InternalEntry, want.InternalEntry) {
		t.Fatal("partial cache hits changed finalized module output")
	}
	stats := cache.Stats()
	if stats.Entries != 4 || stats.Hits != 2 || stats.Misses != 4 {
		t.Fatalf("partial-hit cache stats = %#v on %s", stats, runtime.GOARCH)
	}
}

func TestCompilerFunctionCacheInvalidatesOnlyInlineCallers(t *testing.T) {
	moduleSource := func(opcode byte) []byte {
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
			wasmtest.Section(10, wasmtest.Vec(
				wasmtest.Code([]byte{0x20, 0, 0x20, 1, opcode, 0x0b}),
				wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x10, 0, 0x0b}),
				wasmtest.Code([]byte{0x41, 7, 0x0b}),
			)),
		)
	}
	decode := func(source []byte) *wasm.Module {
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetCompatibility)
	if err != nil {
		t.Fatal(err)
	}
	input := func(source []byte) corecompiler.Input {
		return corecompiler.Input{
			Module: decode(source), Source: source, Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
			Target: target, Objective: corecompiler.ObjectiveSpeed, Bounds: corecompiler.BoundsExplicit,
			ConfigurationFingerprint: [32]byte{1},
		}
	}
	cache := corecompiler.NewFunctionArtifactCache(1 << 20)
	var metrics Metrics
	compiler := Compiler{Metrics: &metrics, FunctionCache: cache}
	if _, err := compiler.Compile(input(moduleSource(0x6a))); err != nil {
		t.Fatal(err)
	}
	changed := input(moduleSource(0x6b))
	got, err := compiler.Compile(changed)
	if err != nil {
		t.Fatal(err)
	}
	uniformStructured := runtime.GOOS == "windows" && runtime.GOARCH == "arm64"
	if len(metrics.Functions) != 3 || uniformStructured && (metrics.Functions[0].CacheHit || metrics.Functions[1].CacheHit || metrics.Functions[2].CacheHit) ||
		!uniformStructured && (metrics.Functions[0].CacheHit || metrics.Functions[1].CacheHit || !metrics.Functions[2].CacheHit) {
		t.Fatalf("inline dependency cache metrics = %#v", metrics.Functions)
	}
	want, err := (Compiler{}).Compile(changed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Code, want.Code) || !equalInts(got.Entry, want.Entry) || !equalInts(got.InternalEntry, want.InternalEntry) {
		t.Fatal("caller-specific cache invalidation changed finalized module output")
	}
}

func TestCompilerFunctionCacheInvalidatesTransitiveIPRACallers(t *testing.T) {
	moduleSource := func(opcode byte) []byte {
		return wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
			wasmtest.Section(10, wasmtest.Vec(
				wasmtest.Code([]byte{0x20, 0, 0x20, 1, opcode, 0x41, 0, 0x6a, 0x0b}),
				wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x10, 0, 0x0b}),
				wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x10, 1, 0x0b}),
				wasmtest.Code([]byte{0x20, 0, 0x0b}),
			)),
		)
	}
	decode := func(source []byte) *wasm.Module {
		m, err := wasm.DecodeModule(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	target, err := corecompiler.HostTarget(corecompiler.TargetCompatibility)
	if err != nil {
		t.Fatal(err)
	}
	input := func(source []byte) corecompiler.Input {
		return corecompiler.Input{
			Module: decode(source), Source: source, Runtime: corecompiler.RuntimeContract{ABIRevision: runtimeabi.Revision},
			Target: target, Objective: corecompiler.ObjectiveSpeed, Bounds: corecompiler.BoundsExplicit,
			ConfigurationFingerprint: [32]byte{1},
		}
	}
	cache := corecompiler.NewFunctionArtifactCache(1 << 20)
	var metrics Metrics
	compiler := Compiler{Metrics: &metrics, FunctionCache: cache}
	if _, err := compiler.Compile(input(moduleSource(0x6a))); err != nil {
		t.Fatal(err)
	}
	changed := input(moduleSource(0x6b))
	got, err := compiler.Compile(changed)
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics.Functions) != 4 || metrics.Functions[0].CacheHit || metrics.Functions[1].CacheHit || metrics.Functions[2].CacheHit || !metrics.Functions[3].CacheHit {
		t.Fatalf("transitive IPRA dependency metrics = %#v", metrics.Functions)
	}
	want, err := (Compiler{}).Compile(changed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Code, want.Code) || !equalInts(got.Entry, want.Entry) || !equalInts(got.InternalEntry, want.InternalEntry) {
		t.Fatal("transitive caller invalidation changed finalized module output")
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}
