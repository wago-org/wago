package settings

import (
	"runtime"
	"testing"
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
	if err := selection.RuntimeConfig().Validate(); err != nil {
		t.Fatalf("runtime config: %v", err)
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
}
