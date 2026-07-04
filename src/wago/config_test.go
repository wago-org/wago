//go:build linux && amd64

package wago

import (
	"errors"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// signExtModule exports f(i32)->i32 = i32.extend8_s(local0).
func signExtModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0xc0, 0x0b}))),
	)
}

func TestConfigDefaultAcceptsSupportedFeatures(t *testing.T) {
	if _, err := Compile(signExtModule()); err != nil {
		t.Fatalf("default config should accept sign-extension: %v", err)
	}
	if _, err := CompileWithConfig(nil, signExtModule()); err != nil {
		t.Fatalf("nil config should use defaults: %v", err)
	}
}

func TestConfigFeatureGatingRejects(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(coreFeaturesWago &^ CoreFeatureSignExtensionOps)
	_, err := CompileWithConfig(cfg, signExtModule())
	if err == nil || !strings.Contains(err.Error(), "sign-extension") {
		t.Fatalf("disabling sign-extension should reject the module, got %v", err)
	}
}

func TestConfigValidationRejectsUnsupported(t *testing.T) {
	cfg := NewRuntimeConfig().WithCoreFeatures(coreFeaturesWago | CoreFeatureSIMD)
	if _, err := CompileWithConfig(cfg, signExtModule()); err == nil {
		t.Fatal("enabling unsupported SIMD should error")
	}
}

func TestConfigSignalsBasedRequiresBuildTag(t *testing.T) {
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased)
	_, err := CompileWithConfig(cfg, signExtModule())
	if guardPageBuilt {
		if err != nil {
			t.Fatalf("signals-based should compile under the build tag: %v", err)
		}
	} else if err == nil || !strings.Contains(err.Error(), "wago_guardpage") {
		t.Fatalf("signals-based without the tag should error, got %v", err)
	}
}

func TestConfigBoundsEnv(t *testing.T) {
	t.Setenv("WAGO_BOUNDS", "signals")
	cfg := NewRuntimeConfig()
	if cfg.BoundsChecks() != BoundsChecksSignalsBased {
		t.Fatalf("WAGO_BOUNDS=signals should select signals-based checks, got %v", cfg.BoundsChecks())
	}
}

func TestConfigImmutable(t *testing.T) {
	base := NewRuntimeConfig()
	baseMode := base.BoundsChecks() // default depends on the build tag; capture it
	derived := base.WithBoundsChecks(BoundsChecksSignalsBased).WithMemoryLimitPages(10)
	if base.BoundsChecks() != baseMode {
		t.Fatal("WithBoundsChecks mutated the base config")
	}
	if derived.BoundsChecks() != BoundsChecksSignalsBased {
		t.Fatal("derived config did not take the new bounds mode")
	}
}

func TestCoreFeaturesBitset(t *testing.T) {
	if !CoreFeaturesV2.IsEnabled(CoreFeatureSignExtensionOps) {
		t.Fatal("V2 should include sign-extension")
	}
	on := CoreFeaturesV1.SetEnabled(CoreFeatureSIMD, true)
	if !on.IsEnabled(CoreFeatureSIMD) {
		t.Fatal("SetEnabled(true) failed")
	}
	if CoreFeaturesV1.IsEnabled(CoreFeatureSIMD) {
		t.Fatal("SetEnabled must not mutate the receiver")
	}
	if off := on.SetEnabled(CoreFeatureSIMD, false); off.IsEnabled(CoreFeatureSIMD) {
		t.Fatal("SetEnabled(false) failed")
	}
}

func TestConfigTypedErrors(t *testing.T) {
	// Unsupported feature -> *UnsupportedFeatureError naming it.
	_, err := NewRuntimeConfig().WithFeature(CoreFeatureSIMD, true).Compile(signExtModule())
	var ufe *UnsupportedFeatureError
	if !errors.As(err, &ufe) {
		t.Fatalf("want *UnsupportedFeatureError, got %T: %v", err, err)
	}
	if !ufe.Requested.IsEnabled(CoreFeatureSIMD) {
		t.Fatalf("error should name simd, got %v", ufe.Requested)
	}
	// Signals-based without the build tag -> GuardPageUnavailableError (default build).
	if !guardPageBuilt {
		err = NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased).Validate()
		if !IsGuardPageUnavailable(err) {
			t.Fatalf("want GuardPageUnavailableError, got %v", err)
		}
	}
}

func TestConfigValidateAndIntrospection(t *testing.T) {
	if err := NewRuntimeConfig().Validate(); err != nil {
		t.Fatalf("default config should validate: %v", err)
	}
	if SupportedFeatures() != coreFeaturesWago {
		t.Fatal("SupportedFeatures mismatch")
	}
	if GuardPageSupported() != guardPageBuilt {
		t.Fatal("GuardPageSupported should mirror the build tag")
	}
	// String is non-empty / informative. The default bounds mode depends on the
	// build tag (explicit normally, signals-based under wago_guardpage).
	if s := NewRuntimeConfig().String(); !strings.Contains(s, "explicit") && !strings.Contains(s, "signals-based") {
		t.Fatalf("config String missing bounds mode: %q", s)
	}
}

func TestConfigCompileMethod(t *testing.T) {
	if _, err := NewRuntimeConfig().Compile(signExtModule()); err != nil {
		t.Fatalf("fluent Compile: %v", err)
	}
}

func TestConfigWithFeatures(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeatures(CoreFeatureMutableGlobal, CoreFeatureSignExtensionOps)
	if !cfg.CoreFeatures().IsEnabled(CoreFeatureMutableGlobal) ||
		!cfg.CoreFeatures().IsEnabled(CoreFeatureSignExtensionOps) {
		t.Fatalf("WithFeatures did not set the union: %v", cfg.CoreFeatures())
	}
	if cfg.CoreFeatures().IsEnabled(CoreFeatureBulkMemoryOperations) {
		t.Fatal("WithFeatures should replace, not add to, the set")
	}
}
