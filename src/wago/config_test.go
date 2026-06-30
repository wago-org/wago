//go:build linux && amd64

package wago

import (
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

func TestConfigImmutable(t *testing.T) {
	base := NewRuntimeConfig()
	derived := base.WithBoundsChecks(BoundsChecksSignalsBased).WithMemoryLimitPages(10)
	if base.BoundsChecks() != BoundsChecksExplicit {
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
