package settings

import (
	"runtime"
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
