package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc"
	"github.com/wago-org/wago/tests/wasmtest"
)

func tierExactRootModule() []byte {
	body := []byte{
		0x01, 0x01, 0x63, 0x00, // one nullable ref-to-type-0 local
		0xfb, 0x01, 0x00, 0x21, 0x00, // struct.new_default 0; local.set 0
		0xfb, 0x01, 0x00, 0x1a, // second allocation; drop
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b, // local.get 0; struct.get 0 0; end
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func tierTestFuncImport(module, name string, typeIdx uint32) []byte {
	out := append(wasmtest.Name(module), wasmtest.Name(name)...)
	out = append(out, 0x00)
	return append(out, wasmtest.ULEB(typeIdx)...)
}

func TestTieringConfigurationEnablesRailshotProfile(t *testing.T) {
	cfg := NewRuntimeConfig().WithTiering(true)
	if !cfg.Tiering() || !cfg.RailshotProfiling() {
		t.Fatalf("tiering/profile = %v/%v, want true/true", cfg.Tiering(), cfg.RailshotProfiling())
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.WithCompiler(CompilerDragline).Validate(); err == nil {
		t.Fatal("Dragline initial-tier configuration unexpectedly validated")
	}
}

func TestRailshotInstanceInstallsSourceIdenticalDraglineTier(t *testing.T) {
	module := draglineScalarModule([]byte{0x20, 0x00, 0x20, 0x01, 0x7c, 0x42, 0x03, 0x85, 0x0b})
	baseConfig := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
	railshot, err := Compile(baseConfig.WithTiering(true), module)
	if err != nil {
		t.Fatal(err)
	}
	defer railshot.Close()
	dragline, err := Compile(baseConfig.WithCompiler(CompilerDragline), module)
	if err != nil {
		t.Fatal(err)
	}
	in, err := Instantiate(railshot, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	prepared, err := in.PrepareFunction("mix")
	if err != nil {
		t.Fatal(err)
	}
	if got := in.ActiveCompiler(); got != CompilerRailshot {
		t.Fatalf("active compiler = %s, want railshot", got)
	}
	before, err := prepared.Invoke2(I64(10), I64(5))
	if err != nil || AsI64(before[0]) != 12 {
		t.Fatalf("Railshot mix(10, 5) = %v, %v", before, err)
	}
	if err := in.InstallDragline(dragline); err != nil {
		t.Fatal(err)
	}
	if got := in.ActiveCompiler(); got != CompilerDragline {
		t.Fatalf("active compiler = %s, want dragline", got)
	}
	// The prepared handle predates installation and therefore proves that its
	// cached address is the stable thunk rather than the original code image.
	after, err := prepared.Invoke2(I64(10), I64(5))
	if err != nil || AsI64(after[0]) != 12 {
		t.Fatalf("Dragline mix(10, 5) = %v, %v", after, err)
	}
	if err := dragline.Close(); err != nil {
		t.Fatal(err)
	}
	afterClose, err := in.Invoke("mix", I64(20), I64(7))
	if err != nil || AsI64(afterClose[0]) != 24 {
		t.Fatalf("retained Dragline mix(20, 7) = %v, %v", afterClose, err)
	}
}

func TestCompilerTierPublishesExactRootGeneration(t *testing.T) {
	if !platformCoreFeatures().IsEnabled(CoreFeatureGC) {
		t.Skip("native Wasm GC execution is unavailable on this target")
	}
	module := tierExactRootModule()
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	railshot, err := Compile(config.WithTiering(true), module)
	if err != nil {
		t.Fatal(err)
	}
	defer railshot.Close()
	candidate, err := Compile(config.WithCompiler(CompilerDragline), module)
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	if !railshot.GCNativeRootAdmission().Exact || !candidate.GCNativeRootAdmission().Exact {
		t.Skip("exact native GC root maps are unavailable on this target")
	}
	in, err := Instantiate(railshot, InstantiateOptions{GC: gc.Config{Profile: gc.ProfileThroughput}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := in.InstallDragline(candidate); err != nil {
		t.Fatal(err)
	}
	generation := in.profile.tier.installed.Load()
	if generation == nil || generation.compiled != candidate || generation.compiled.genericGCFrameRoots() != candidate.genericGCFrameRoots() {
		t.Fatalf("installed compiler generation = %+v", generation)
	}
	if got, err := in.Invoke("run"); err != nil || len(got) != 1 || got[0] != 0 {
		t.Fatalf("tiered GC run = %v, %v", got, err)
	}
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
}

func TestCompilerPartialTierPopulatesManagedReferenceGenerations(t *testing.T) {
	if !platformCoreFeatures().IsEnabled(CoreFeatureGC) {
		t.Skip("native Wasm GC execution is unavailable on this target")
	}
	body := []byte{
		0x01, 0x01, 0x63, 0x00,
		0xfb, 0x01, 0x00, 0x21, 0x00,
		0xfb, 0x01, 0x00, 0x1a,
		0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x0b,
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x01},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("selected", 0, 0),
			wasmtest.ExportEntry("railshot", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(body))), body...),
			append(wasmtest.ULEB(uint32(len(body))), body...),
		)),
	)
	config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	railshot, err := Compile(config.WithTiering(true), module)
	if err != nil {
		t.Fatal(err)
	}
	defer railshot.Close()
	candidate, err := CompileNativeClone(config, module, CompilerTierPlan{Functions: []uint32{0}})
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	if !railshot.GCNativeRootAdmission().Exact || !candidate.GCNativeRootAdmission().Exact {
		t.Skip("exact native GC root maps are unavailable on this target")
	}
	if candidate.Entry[0] == 0 || candidate.InternalEntry[0] == 0 || candidate.Entry[1] != 0 || candidate.InternalEntry[1] != 0 {
		t.Fatalf("compact GC entries=%v internal=%v", candidate.Entry, candidate.InternalEntry)
	}
	in, err := Instantiate(railshot, InstantiateOptions{GC: gc.Config{
		Profile: gc.ProfileThroughput, CollectEveryAlloc: true,
		StressNurseryBytes: 4096, VerifyAfterCollect: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.SnapshotRailshotProfile(CompilerProfileStartup, true); err != nil {
		t.Fatal(err)
	}
	if err := in.InstallDraglineTier(candidate, CompilerTierPlan{Functions: []uint32{0}}); err != nil {
		t.Fatal(err)
	}
	generation := in.profile.tier.installed.Load()
	if generation == nil || generation.compiled != candidate || generation.compiled.genericGCFrameRoots() != candidate.genericGCFrameRoots() {
		t.Fatalf("partial installed compiler generation = %+v", generation)
	}
	for _, export := range []string{"selected", "railshot"} {
		got, err := in.Invoke(export)
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("%s() = %v, %v; want [0]", export, got, err)
		}
	}
	profile, err := in.SnapshotRailshotProfile(CompilerProfileSteady, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := profile.FunctionCounts; len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("partial managed-reference Railshot counts = %v, want [0 1]", got)
	}
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
}

func TestCompilerTierRejectsDifferentSourceAndSecondInstall(t *testing.T) {
	module := draglineScalarModule([]byte{0x20, 0x00, 0x20, 0x01, 0x7c, 0x0b})
	railshot, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithTiering(true), module)
	if err != nil {
		t.Fatal(err)
	}
	defer railshot.Close()
	in, err := Instantiate(railshot, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	different, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithCompiler(CompilerDragline), draglineScalarModule([]byte{0x20, 0x00, 0x20, 0x01, 0x7e, 0x0b}))
	if err != nil {
		t.Fatal(err)
	}
	defer different.Close()
	if err := in.InstallDragline(different); err == nil {
		t.Fatal("different-source Dragline tier unexpectedly installed")
	}
	matching, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithCompiler(CompilerDragline), module)
	if err != nil {
		t.Fatal(err)
	}
	defer matching.Close()
	if err := in.InstallDragline(matching); err != nil {
		t.Fatal(err)
	}
	if err := in.InstallDragline(matching); err == nil {
		t.Fatal("second Dragline tier unexpectedly installed")
	}
}

func TestTierableArtifactsPreserveSourceIdentity(t *testing.T) {
	module := draglineScalarModule([]byte{0x20, 0x00, 0x20, 0x01, 0x7c, 0x0b})
	compileLoad := func(t *testing.T, cfg *RuntimeConfig) *Compiled {
		t.Helper()
		compiled, err := Compile(cfg, module)
		if err != nil {
			t.Fatal(err)
		}
		artifact, err := compiled.MarshalBinary()
		compiled.Close()
		if err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(artifact)
		if err != nil {
			t.Fatal(err)
		}
		return loaded
	}
	railshot := compileLoad(t, NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithTiering(true))
	defer railshot.Close()
	dragline := compileLoad(t, NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithCompiler(CompilerDragline))
	defer dragline.Close()
	in, err := Instantiate(railshot, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := in.InstallDragline(dragline); err != nil {
		t.Fatal(err)
	}
}

func TestCrossInstanceImportFollowsInstalledDraglineTier(t *testing.T) {
	producerModule := draglineScalarModule([]byte{0x20, 0x00, 0x20, 0x01, 0x7c, 0x0b})
	consumerModule := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(2, wasmtest.Vec(tierTestFuncImport("env", "mix", 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x10, 0x00, 0x0b}))),
	)
	base := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
	railshot, err := Compile(base.WithTiering(true), producerModule)
	if err != nil {
		t.Fatal(err)
	}
	defer railshot.Close()
	dragline, err := Compile(base.WithCompiler(CompilerDragline), producerModule)
	if err != nil {
		t.Fatal(err)
	}
	defer dragline.Close()
	producer, err := Instantiate(railshot, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	exported, err := producer.ExportedFunc("mix")
	if err != nil {
		t.Fatal(err)
	}
	consumerCode, err := Compile(base, consumerModule)
	if err != nil {
		t.Fatal(err)
	}
	defer consumerCode.Close()
	consumer, err := Instantiate(consumerCode, InstantiateOptions{Imports: Imports{"env.mix": exported}})
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	for _, install := range []bool{false, true} {
		if install {
			if err := producer.InstallDragline(dragline); err != nil {
				t.Fatal(err)
			}
		}
		got, err := consumer.Invoke("run", I64(19), I64(23))
		if err != nil || AsI64(got[0]) != 42 {
			t.Fatalf("cross-instance install=%v result=%v error=%v", install, got, err)
		}
	}
}

func TestInstallDraglineConcurrentWithNativeEntries(t *testing.T) {
	module := draglineScalarModule([]byte{0x20, 0x00, 0x20, 0x01, 0x7c, 0x0b})
	base := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
	railshot, err := Compile(base.WithTiering(true), module)
	if err != nil {
		t.Fatal(err)
	}
	defer railshot.Close()
	dragline, err := Compile(base.WithCompiler(CompilerDragline), module)
	if err != nil {
		t.Fatal(err)
	}
	defer dragline.Close()
	in, err := Instantiate(railshot, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	prepared, err := in.PrepareFunction("mix")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		for range 2000 {
			got, callErr := prepared.Invoke2(I64(19), I64(23))
			if callErr != nil {
				done <- callErr
				return
			}
			if AsI64(got[0]) != 42 {
				done <- fmt.Errorf("mix result %d, want 42", AsI64(got[0]))
				return
			}
		}
		done <- nil
	}()
	<-started
	if err := in.InstallDragline(dragline); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestInstallDraglineTierPublishesOnlySelectedFunctions(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("add", 0, 0),
			wasmtest.ExportEntry("mul", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x7c, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x7e, 0x0b}),
		)),
	)
	base := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
	railshot, err := Compile(base.WithTiering(true), module)
	if err != nil {
		t.Fatal(err)
	}
	defer railshot.Close()
	dragline, err := Compile(base.WithCompiler(CompilerDragline), module)
	if err != nil {
		t.Fatal(err)
	}
	defer dragline.Close()
	in, err := Instantiate(railshot, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.SnapshotRailshotProfile(CompilerProfileStartup, true); err != nil {
		t.Fatal(err)
	}
	if err := in.InstallDraglineTier(dragline, CompilerTierPlan{Functions: []uint32{0}}); err != nil {
		t.Fatal(err)
	}
	add, err := in.Invoke("add", I64(6), I64(7))
	if err != nil || AsI64(add[0]) != 13 {
		t.Fatalf("add = %v, %v", add, err)
	}
	mul, err := in.Invoke("mul", I64(6), I64(7))
	if err != nil || AsI64(mul[0]) != 42 {
		t.Fatalf("mul = %v, %v", mul, err)
	}
	profile, err := in.SnapshotRailshotProfile(CompilerProfileSteady, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := profile.FunctionCounts; len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("post-install Railshot counts = %v, want [0 1]", got)
	}
}

func TestCompactNativeCloneCompilesAndInstallsOnlySelectedFunction(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("add", 0, 0),
			wasmtest.ExportEntry("mul", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x7c, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x7e, 0x0b}),
		)),
	)
	base := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
	railshot, err := Compile(base.WithTiering(true), append([]byte(nil), module...))
	if err != nil {
		t.Fatal(err)
	}
	defer railshot.Close()
	full, err := Compile(base.WithCompiler(CompilerDragline).WithTarget(TargetNative), append([]byte(nil), module...))
	if err != nil {
		t.Fatal(err)
	}
	defer full.Close()
	plan := CompilerTierPlan{Roots: []uint32{0}, Functions: []uint32{0}}
	clone, err := CompileNativeClone(base, append([]byte(nil), module...), plan)
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Close()
	if clone.CodeSize() >= full.CodeSize() || clone.Entry[0] == 0 || clone.InternalEntry[0] == 0 || clone.Entry[1] != 0 || clone.InternalEntry[1] != 0 {
		t.Fatalf("compact/full bytes = %d/%d entries=%v internal=%v", clone.CodeSize(), full.CodeSize(), clone.Entry, clone.InternalEntry)
	}
	if _, err := Instantiate(clone, InstantiateOptions{}); err == nil {
		t.Fatal("compact clone unexpectedly instantiated standalone")
	}
	if _, err := clone.MarshalBinary(); err == nil {
		t.Fatal("compact clone unexpectedly serialized")
	}
	in, err := Instantiate(railshot, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.SnapshotRailshotProfile(CompilerProfileStartup, true); err != nil {
		t.Fatal(err)
	}
	if err := in.InstallDraglineTier(clone, CompilerTierPlan{Functions: []uint32{1}}); err == nil {
		t.Fatal("compact clone unexpectedly installed under a different selection")
	}
	if err := in.InstallDraglineTier(clone, plan); err != nil {
		t.Fatal(err)
	}
	add, err := in.Invoke("add", I64(6), I64(7))
	if err != nil || AsI64(add[0]) != 13 {
		t.Fatalf("native-clone add = %v, %v", add, err)
	}
	mul, err := in.Invoke("mul", I64(6), I64(7))
	if err != nil || AsI64(mul[0]) != 42 {
		t.Fatalf("compatibility mul = %v, %v", mul, err)
	}
	profile, err := in.SnapshotRailshotProfile(CompilerProfileSteady, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := profile.FunctionCounts; len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("compact-clone Railshot counts = %v, want [0 1]", got)
	}
}

func TestCompactNativeCloneRequiresDirectCallClosure(t *testing.T) {
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("caller", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 0x01, 0x0b}),
			wasmtest.Code([]byte{0x41, 0x07, 0x0b}),
		)),
	)
	base := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
	if _, err := CompileNativeClone(base, append([]byte(nil), module...), CompilerTierPlan{Functions: []uint32{0}}); err == nil {
		t.Fatal("direct-call-open native clone unexpectedly compiled")
	}
	closed, err := CompileNativeClone(base, append([]byte(nil), module...), CompilerTierPlan{Functions: []uint32{0, 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer closed.Close()
	railshot, err := Compile(base.WithTiering(true), append([]byte(nil), module...))
	if err != nil {
		t.Fatal(err)
	}
	defer railshot.Close()
	in, err := Instantiate(railshot, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := in.InstallDraglineTier(closed, CompilerTierPlan{Functions: []uint32{0, 1}}); err != nil {
		t.Fatal(err)
	}
	got, err := in.Invoke("caller")
	if err != nil || len(got) != 1 || AsI32(got[0]) != 7 {
		t.Fatalf("compact direct-call clone = %v, %v", got, err)
	}
}

func TestInstallDraglineTierRejectsMalformedPlanBeforePublication(t *testing.T) {
	module := draglineScalarModule([]byte{0x20, 0x00, 0x20, 0x01, 0x7c, 0x0b})
	base := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
	railshot, err := Compile(base.WithTiering(true), module)
	if err != nil {
		t.Fatal(err)
	}
	defer railshot.Close()
	dragline, err := Compile(base.WithCompiler(CompilerDragline), module)
	if err != nil {
		t.Fatal(err)
	}
	defer dragline.Close()
	in, err := Instantiate(railshot, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := in.InstallDraglineTier(dragline, CompilerTierPlan{Functions: []uint32{1}}); err == nil {
		t.Fatal("out-of-range tier plan unexpectedly installed")
	}
	if err := in.InstallDraglineTier(dragline, CompilerTierPlan{Functions: []uint32{0}}); err != nil {
		t.Fatalf("valid plan after rejected plan: %v", err)
	}
}
