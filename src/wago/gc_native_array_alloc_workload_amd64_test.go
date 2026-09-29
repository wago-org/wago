//go:build linux && amd64 && !tinygo && !wago_guardpage

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

	const iterations = 128
	for i := 0; i < iterations; i++ {
		if _, err := in.Invoke("fixed"); err != nil {
			t.Fatal(err)
		}
		if _, err := in.Invoke("uniform"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGCNativeArrayDynamicAndLargeShapes(t *testing.T) {
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

		for i := uint64(0); i < 16; i++ {
			if got, err := in.Invoke("set_get", 3, 1, i); err != nil || len(got) != 1 || got[0] != i {
				t.Fatalf("set_get(%d) = %v, %v", i, got, err)
			}
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

		if _, err := in.Invoke("run"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestGCNativeArrayAllocPreservesAllocationCount(t *testing.T) {
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

	const iterations = 32
	for i := 0; i < iterations; i++ {
		got, err := in.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("iteration %d = %v, %v", i, got, err)
		}
	}
	const allocations = iterations * 33

	if got := in.gc.Stats().Allocations; got != uint64(allocations) {
		t.Fatalf("semantic allocations = %d, want %d", got, allocations)
	}
}
