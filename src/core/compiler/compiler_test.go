package compiler

import (
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/profile"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	runtimeabi "github.com/wago-org/wago/src/core/runtime/abi"
)

func TestTargetFingerprintCoversCodeGenerationIdentity(t *testing.T) {
	base := Target{GOOS: "linux", GOARCH: "amd64", Mode: TargetCompatibility}
	want := base.Fingerprint()
	if want != base.Fingerprint() {
		t.Fatal("target fingerprint is not deterministic")
	}
	variants := []Target{
		{GOOS: "darwin", GOARCH: "amd64", Mode: TargetCompatibility},
		{GOOS: "linux", GOARCH: "arm64", Mode: TargetCompatibility},
		{GOOS: "linux", GOARCH: "amd64", Mode: TargetNative},
		{GOOS: "linux", GOARCH: "amd64", Mode: TargetCompatibility, CPUModel: "zen5"},
		{GOOS: "linux", GOARCH: "amd64", Mode: TargetCompatibility, TuningModel: "znver5"},
		{GOOS: "linux", GOARCH: "amd64", Mode: TargetCompatibility, FeatureBits: [4]uint64{1}},
	}
	for _, variant := range variants {
		if got := variant.Fingerprint(); got == want {
			t.Fatalf("target variant shares fingerprint with base: %#v", variant)
		}
	}
}

func TestTargetFeatureIdentitiesRemainStable(t *testing.T) {
	if TargetFeatureAMD64BMI2 != 0 || TargetFeatureARM64SVE2 != 9 || TargetFeatureAMD64APX != 10 || TargetFeatureARM64MOPS != 11 {
		t.Fatalf("target feature identities changed: BMI2=%d SVE2=%d APX=%d MOPS=%d", TargetFeatureAMD64BMI2, TargetFeatureARM64SVE2, TargetFeatureAMD64APX, TargetFeatureARM64MOPS)
	}
	var target Target
	target.setFeature(TargetFeatureAMD64APX, true)
	target.setFeature(TargetFeatureARM64MOPS, true)
	if !target.HasFeature(TargetFeatureAMD64APX) || !target.HasFeature(TargetFeatureARM64MOPS) || target.HasFeature(TargetFeatureARM64SVE2) {
		t.Fatalf("target feature bitset = %#x", target.FeatureBits)
	}
}

func TestHostTargetModesAreDistinctAndNativeFeaturesAreExact(t *testing.T) {
	compat, err := HostTarget(TargetCompatibility)
	if err != nil {
		t.Fatal(err)
	}
	native, err := HostTarget(TargetNative)
	if err != nil {
		t.Fatal(err)
	}
	if compat.Mode != TargetCompatibility || native.Mode != TargetNative {
		t.Fatalf("target modes = %s, %s", compat.Mode, native.Mode)
	}
	if compat.FeatureBits != [4]uint64{} {
		t.Fatalf("compatibility features = %#x, want baseline", compat.FeatureBits)
	}
	if native.CPUModel == "" || native.CPUModel == "host" || native.TuningModel == "" {
		t.Fatalf("native CPU identity = model %q tuning %q, want canonical model and tuning family", native.CPUModel, native.TuningModel)
	}
	if compat.Fingerprint() == native.Fingerprint() {
		t.Fatal("native and compatibility target identities collide")
	}
	if _, err := HostTarget(TargetExplicit); err == nil {
		t.Fatal("explicit target resolved without an explicit configuration")
	}
}

func TestClassifyHostCPUProducesCanonicalStableIdentity(t *testing.T) {
	tests := []struct {
		brand, arch, model, tuning string
	}{
		{"Apple M4 Max", "arm64", "apple-m4-max", "apple-m4"},
		{" Apple M2 Pro ", "arm64", "apple-m2-pro", "apple-m2"},
		{"AMD Ryzen 9 9950X 16-Core Processor", "amd64", "amd-ryzen-9-9950x-16-core-processor", "generic-amd64"},
		{"", "arm64", "host-arm64", "generic-arm64"},
	}
	for _, test := range tests {
		model, tuning := classifyHostCPU(test.brand, test.arch)
		if model != test.model || tuning != test.tuning {
			t.Fatalf("classifyHostCPU(%q, %q) = %q, %q; want %q, %q", test.brand, test.arch, model, tuning, test.model, test.tuning)
		}
	}
}

func TestRouterSelectsOneEngineAndStampsOutput(t *testing.T) {
	m := &wasm.Module{}
	calls := [2]int{}
	router := Router{
		Railshot: BackendFunc(func(Input) (Output, error) { calls[0]++; return Output{}, nil }),
		Dragline: BackendFunc(func(Input) (Output, error) { calls[1]++; return Output{}, nil }),
	}
	output, err := router.Compile(EngineDragline, Input{Module: m, Runtime: RuntimeContract{ABIRevision: runtimeabi.Revision}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != [2]int{0, 1} {
		t.Fatalf("backend calls = %v, want only Dragline", calls)
	}
	if output.Engine != EngineDragline {
		t.Fatalf("output engine = %v, want dragline", output.Engine)
	}
}

func TestRouterRejectsInvalidBoundaryInputs(t *testing.T) {
	backendErr := errors.New("strict failure")
	router := Router{Dragline: BackendFunc(func(Input) (Output, error) { return Output{}, backendErr })}
	valid := Input{Module: &wasm.Module{}, Runtime: RuntimeContract{ABIRevision: runtimeabi.Revision}}
	if _, err := router.Compile(Engine(99), valid); err == nil {
		t.Fatal("unknown engine accepted")
	}
	if _, err := router.Compile(EngineDragline, Input{Runtime: valid.Runtime}); err == nil {
		t.Fatal("nil validated module accepted")
	}
	wrongABI := valid
	wrongABI.Runtime.ABIRevision++
	if _, err := router.Compile(EngineDragline, wrongABI); err == nil {
		t.Fatal("wrong runtime ABI accepted")
	}
	if _, err := router.Compile(EngineDragline, valid); !errors.Is(err, backendErr) {
		t.Fatalf("backend error = %v, want wrapped sentinel", err)
	}
	profiled := valid
	profiled.Source = []byte("source")
	profiled.Profile = &profile.Module{Version: profile.Version, ModuleHash: sha256.Sum256([]byte("other")), Source: profile.SourceStatic, Phase: profile.PhaseStartup}
	if _, err := router.Compile(EngineDragline, profiled); err == nil {
		t.Fatal("profile for a different source generation was accepted")
	}
	hostModule := &wasm.Module{Imports: []wasm.Import{{Type: wasm.NewFuncExternType(wasm.TypeIdx{})}}}
	invalidHostCount := valid
	invalidHostCount.Module = hostModule
	invalidHostCount.HostEffects = []HostEffectBinding{{}, {}}
	if _, err := router.Compile(EngineDragline, invalidHostCount); err == nil {
		t.Fatal("misindexed host effect contracts were accepted")
	}
	invalidHostBits := valid
	invalidHostBits.Module = hostModule
	invalidHostBits.HostEffects = []HostEffectBinding{{Declared: true, Contract: HostEffectContract{Reads: HostHeapMask(1 << 15)}}}
	if _, err := router.Compile(EngineDragline, invalidHostBits); err == nil {
		t.Fatal("unknown host effect bits were accepted")
	}
}
