//go:build linux && amd64 && !tinygo && !wago_guardpage && wago_gcstats

package wago

import "testing"

func TestGCNativeReferenceArrayAllocUsesExactNativeInitializers(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeReferenceArrayModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	in.SetGCHelperStatsTracking(true)
	defer in.SetGCHelperStatsTracking(false)
	const iterations = 128
	for i := 0; i < iterations; i++ {
		if _, err := in.Invoke("fixed"); err != nil {
			t.Fatal(err)
		}
		if _, err := in.Invoke("uniform"); err != nil {
			t.Fatal(err)
		}
	}
	stats := in.GCHelperStats()
	if stats.ArrayAllocationCalls != 4*iterations {
		t.Fatalf("reference array helper calls = %d, want %d for unamortized two-array invocations", stats.ArrayAllocationCalls, 4*iterations)
	}
}

func TestGCNativeArrayDynamicAndLargeShapesStayHelperOnly(t *testing.T) {
	t.Run("dynamic", func(t *testing.T) {
		compiled, err := compileStagedGCArray(stagedGCArrayNumericLocalBytes(t))
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		in, err := instantiateCore(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		in.SetGCHelperStatsTracking(true)
		defer in.SetGCHelperStatsTracking(false)
		for i := uint64(0); i < 16; i++ {
			if _, err := in.Invoke("set_get", 3, 1, i); err != nil {
				t.Fatal(err)
			}
		}
		if got := in.GCHelperStats().ArrayAllocationCalls; got != 16 {
			t.Fatalf("dynamic array helper calls = %d, want 16", got)
		}
	})
	t.Run("large-static", func(t *testing.T) {
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeArrayDefaultBenchmarkModule([]byte{0x5e, 0x7f, 0x01}, 256))
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		in, err := Instantiate(compiled, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		in.SetGCHelperStatsTracking(true)
		defer in.SetGCHelperStatsTracking(false)
		if _, err := in.Invoke("run"); err != nil {
			t.Fatal(err)
		}
		if got := in.GCHelperStats().ArrayAllocationCalls; got != 33 {
			t.Fatalf("large static array helper calls = %d, want 33", got)
		}
	})
}

func TestGCNativeArrayAllocAvoidsMostGoHelpers(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeArrayDefaultBenchmarkModule([]byte{0x5e, 0x7f, 0x01}, 4))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	in.SetGCHelperStatsTracking(true)
	defer in.SetGCHelperStatsTracking(false)
	const iterations = 32
	for i := 0; i < iterations; i++ {
		got, err := in.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("iteration %d = %v, %v", i, got, err)
		}
	}
	const allocations = iterations * 33
	stats := in.GCHelperStats()
	if stats.ArrayAllocationCalls != 9*iterations {
		t.Fatalf("array allocation helper calls = %d, want %d for %d allocations", stats.ArrayAllocationCalls, 9*iterations, allocations)
	}
	if got := in.gc.Stats().Allocations; got != uint64(allocations) {
		t.Fatalf("semantic allocations = %d, want %d", got, allocations)
	}
}
