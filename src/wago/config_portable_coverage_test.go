package wago

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestRuntimeConfigPortableFluentSurface(t *testing.T) {
	const emptyModule = "\x00asm\x01\x00\x00\x00"
	base := NewRuntimeConfig()
	cfg := base.WithCoreFeatures(CoreFeatureMutableGlobal).
		WithFeatures(CoreFeatureMutableGlobal, CoreFeatureSignExtensionOps).
		WithFeature(CoreFeatureSIMD, false).
		WithMemoryLimitPages(3).
		WithBoundsChecks(BoundsChecksExplicit).
		WithDeferBoundsChecks(false)
	if base == cfg || base.MemoryLimitPages() == 3 {
		t.Fatal("fluent methods mutated the base config")
	}
	if cfg.CoreFeatures() != CoreFeatureMutableGlobal|CoreFeatureSignExtensionOps || cfg.BoundsChecks() != BoundsChecksExplicit || cfg.DeferBoundsChecks() || cfg.MemoryLimitPages() != 3 {
		t.Fatalf("fluent config mismatch: %+v", cfg)
	}
	if !strings.Contains(cfg.String(), "maxMemoryPages: 3") {
		t.Fatalf("config String = %q", cfg.String())
	}
	if _, err := cfg.Compile([]byte(emptyModule)); err != nil {
		t.Fatalf("fluent Compile: %v", err)
	}
	if cfg.MustCompile([]byte(emptyModule)) == nil {
		t.Fatal("MustCompile returned nil")
	}
	for _, tc := range []struct {
		mode BoundsCheckMode
		want string
	}{{BoundsChecksExplicit, "explicit"}, {BoundsChecksSignalsBased, "signals-based"}, {BoundsCheckMode(99), "BoundsCheckMode(99)"}} {
		if got := tc.mode.String(); got != tc.want {
			t.Errorf("BoundsCheckMode(%d).String() = %q", tc.mode, got)
		}
	}
	if got := (&GuardPageUnavailableError{}).Error(); !strings.Contains(got, "signals-based") {
		t.Fatalf("GuardPageUnavailableError = %q", got)
	}
	if got := (&UnsupportedFeatureError{Requested: CoreFeatureTailCall, Supported: CoreFeaturesV2}).Error(); !strings.Contains(got, "tail-call") {
		t.Fatalf("UnsupportedFeatureError = %q", got)
	}
	err := NewRuntimeConfig().WithFeature(CoreFeatureTailCall, true).Validate()
	var unsupported *UnsupportedFeatureError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Validate unsupported = %v", err)
	}
	if !guardPageBuilt {
		err = NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased).Validate()
		if !IsGuardPageUnavailable(err) {
			t.Fatalf("Validate signals = %v", err)
		}
	}
}

func TestSIMDHostDetectionMatchesArchitectureBaseline(t *testing.T) {
	got := detectSIMDHostFeatures()
	if runtime.GOARCH == "arm64" && !got {
		t.Fatal("arm64 baseline Advanced SIMD was rejected")
	}
	if runtime.GOARCH != "arm64" && runtime.GOARCH != "amd64" && got {
		t.Fatalf("unsupported architecture %s admitted SIMD", runtime.GOARCH)
	}
}

func TestRuntimeBuildCapabilitiesAndOptimizationKnobs(t *testing.T) {
	supported := SupportedFeatures()
	if supported&^coreFeaturesWago != 0 || (hostSupportsSIMD() && supported&CoreFeatureSIMD == 0) {
		t.Fatalf("supported features = %s", supported)
	}
	if GuardPageSupported() != guardPageBuilt {
		t.Fatal("guard-page build capability disagrees with build flag")
	}
	knobs := OptKnobs()
	if len(knobs) == 0 || knobs[0].Name == "" || knobs[0].Desc == "" {
		t.Fatalf("optimization knobs = %#v", knobs)
	}
	original := knobs[0].On
	if !SetOptKnob(knobs[0].Name, !original) {
		t.Fatalf("could not set known knob %q", knobs[0].Name)
	}
	if got := OptKnobs()[0].On; got != !original {
		t.Fatalf("knob %q = %v, want %v", knobs[0].Name, got, !original)
	}
	if !SetOptKnob(knobs[0].Name, original) || SetOptKnob("not-a-knob", true) {
		t.Fatal("optimization knob setter result changed")
	}
}

func TestGuardedMemoryFallbackExplainsBuildRequirement(t *testing.T) {
	if guardPageBuilt {
		t.Skip("guard-page build supplies real guarded memory")
	}
	if _, err := newGuardedJobMemory(1, 1); err == nil {
		t.Fatal("guarded memory unexpectedly available without wago_guardpage")
	}
}
