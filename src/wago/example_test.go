//go:build linux && amd64

package wago_test

import (
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	wago "github.com/wago-org/wago/src/wago"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// addModule exports add(i32, i32) -> i32.
func addModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(
			[]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("add", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b}))),
	)
}

// Compile, instantiate, and invoke — the standard path.
func Example_compileAndInvoke() {
	mod, err := wago.Compile(nil, addModule())
	if err != nil {
		panic(err)
	}
	inst, err := wago.Instantiate(mod, wago.InstantiateOptions{})
	if err != nil {
		panic(err)
	}
	defer inst.Close()
	out, _ := inst.Invoke("add", wago.I32(40), wago.I32(2))
	fmt.Println(wago.AsI32(out[0]))
	// Output: 42
}

// SupportedFeatures reports what this build and host can compile.
func ExampleSupportedFeatures() {
	features := wago.SupportedFeatures()
	fmt.Println(features.IsEnabled(wago.CoreFeatureMutableGlobal))
	fmt.Println(features.IsEnabled(wago.CoreFeatureTailCall))
	// Output:
	// true
	// false
}

// A config is immutable; WithFeature returns a copy with one proposal toggled.
func ExampleRuntimeConfig_WithFeature() {
	cfg := wago.NewRuntimeConfig().WithFeature(wago.CoreFeatureBulkMemoryOperations, false)
	fmt.Println(cfg.CoreFeatures())
	// Output: multi-value|mutable-global|nontrapping-float-to-int-conversion|reference-types|sign-extension-ops|simd|extended-const-expressions
}

func ExampleCoreFeatures_IsEnabled() {
	feats := wago.CoreFeaturesV2
	fmt.Println(feats.IsEnabled(wago.CoreFeatureSignExtensionOps))
	fmt.Println(feats.IsEnabled(wago.CoreFeatureSIMD))
	// Output:
	// true
	// true
}

// Pick the fastest bounds-check mode the build supports, with no hard failure.
func ExampleGuardPageSupported() {
	cfg := wago.NewRuntimeConfig()
	if wago.GuardPageSupported() {
		cfg = cfg.WithBoundsChecks(wago.BoundsChecksSignalsBased)
	}
	_, _ = cfg.Compile(addModule())
}
