package settings

import (
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago"
)

func TestResolveCompilationOwnsPrecedence(t *testing.T) {
	config := Default()
	config.Runtime.Parallel = "8"
	config.Runtime.DeferredBoundsChecking = false
	optimizations := OptimizationsForArch(runtime.GOARCH)
	if len(optimizations) == 0 {
		t.Fatal("current architecture has no Optimization Bindings")
	}
	name := optimizations[0].Name()
	config.Optimizations[name] = false
	explicitDeferred := true
	selection, err := ResolveCompilationFrom(config, true, CompilationRequest{
		Arch: runtime.GOARCH, Core: "3", Parallel: "2", DeferredBoundsChecking: &explicitDeferred,
		Optimizations: map[string]bool{name: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Core != 3 || selection.FunctionWorkers != 2 || !selection.DeferredBoundsChecking || !selection.Optimizations[name] {
		t.Fatalf("selection = %#v", selection)
	}
	if got := selection.RuntimeConfig().CoreFeatures(); !got.IsEnabled(wago.CoreFeaturesV3) {
		t.Fatalf("runtime config features = %s, missing Core 3 set %s", got, wago.CoreFeaturesV3)
	}
}

func TestResolveCompilationFiltersTargetOptimizations(t *testing.T) {
	config := Default()
	selection, err := ResolveCompilationFrom(config, true, CompilationRequest{Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	for name := range selection.Optimizations {
		if _, ok := Lookup("optimizations." + name); !ok {
			t.Fatalf("unknown selected optimization %q", name)
		}
	}
	if _, err := ResolveCompilationFrom(config, true, CompilationRequest{Arch: "amd64", Optimizations: map[string]bool{"arm64-only-missing": true}}); err == nil {
		t.Fatal("unsupported target optimization was accepted")
	}
	if err := selection.RuntimeConfig().Validate(); err != nil {
		t.Fatalf("supported runtime config: %v", err)
	}
}

func TestCoreSelectionDefaultsAndExplicitRelease2(t *testing.T) {
	selection, err := ResolveCompilationFrom(Default(), false, CompilationRequest{Arch: runtime.GOARCH})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Core != 0 {
		t.Fatalf("automatic core selection = %d, want 0", selection.Core)
	}
	if got := selection.RuntimeConfig().CoreFeatures(); got != wago.NewRuntimeConfig().CoreFeatures() {
		t.Fatalf("automatic features = %s, want runtime default %s", got, wago.NewRuntimeConfig().CoreFeatures())
	}

	selection, err = ResolveCompilationFrom(Default(), false, CompilationRequest{Arch: runtime.GOARCH, Core: "2"})
	if err != nil {
		t.Fatal(err)
	}
	if got := selection.RuntimeConfig().CoreFeatures(); got != wago.CoreFeaturesV2 {
		t.Fatalf("explicit Core 2 features = %s, want %s", got, wago.CoreFeaturesV2)
	}
}

func TestBackendSelectionIsExperimentalAndStrict(t *testing.T) {
	config := Default()
	config.Experimental["dragline"] = true
	selection, err := ResolveCompilationFrom(config, true, CompilationRequest{Arch: runtime.GOARCH, Backend: "dragline"})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Backend != wago.CompilerDragline || selection.RuntimeConfig().Compiler() != wago.CompilerDragline {
		t.Fatalf("selection = %#v", selection)
	}
	if _, err := ResolveCompilationFrom(Default(), false, CompilationRequest{Arch: runtime.GOARCH, Backend: "dragline"}); err == nil || !strings.Contains(err.Error(), "wago config --enable dragline --experimental") {
		t.Fatalf("Dragline backend without opt-in error = %v", err)
	}
	if _, err := ResolveCompilationFrom(Default(), false, CompilationRequest{Arch: runtime.GOARCH, Backend: "missing"}); err == nil {
		t.Fatal("unknown backend accepted")
	}
}

func TestCompilerTargetSelectionIsOrthogonal(t *testing.T) {
	config := Default()
	config.Experimental["dragline"] = true
	selection, err := ResolveCompilationFrom(config, true, CompilationRequest{
		Arch: runtime.GOARCH, Backend: "dragline", Target: "native", Fallback: "railshot", Objective: "size",
	})
	if err != nil {
		t.Fatal(err)
	}
	if selection.Backend != wago.CompilerDragline || selection.Target != wago.TargetNative || selection.Fallback != wago.CompilerFallbackRailshot || selection.Objective != wago.OptimizeSize {
		t.Fatalf("selection = %#v", selection)
	}
	runtimeConfig := selection.RuntimeConfig()
	if runtimeConfig.Compiler() != wago.CompilerDragline || runtimeConfig.CompilerTarget() != wago.TargetNative || runtimeConfig.CompilerFallback() != wago.CompilerFallbackRailshot || runtimeConfig.OptimizationObjective() != wago.OptimizeSize {
		t.Fatalf("runtime config compiler=%s target=%s objective=%s fallback=%s", runtimeConfig.Compiler(), runtimeConfig.CompilerTarget(), runtimeConfig.OptimizationObjective(), runtimeConfig.CompilerFallback())
	}
	if _, err := ResolveCompilationFrom(Default(), false, CompilationRequest{Arch: runtime.GOARCH, Target: "host-ish"}); err == nil {
		t.Fatal("unknown compiler target accepted")
	}
	if _, err := ResolveCompilationFrom(Default(), false, CompilationRequest{Arch: runtime.GOARCH, Fallback: "railshot"}); err == nil {
		t.Fatal("Railshot fallback without Dragline was accepted")
	}
	if _, err := ResolveCompilationFrom(Default(), false, CompilationRequest{Arch: runtime.GOARCH, Objective: "fastish"}); err == nil {
		t.Fatal("unknown optimization objective was accepted")
	}
}
