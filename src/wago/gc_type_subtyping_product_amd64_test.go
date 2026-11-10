//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"testing"
)

func compileStagedGCTypeSubtypingProductForTest(data []byte) (*Compiled, error) {
	cfg := compatibilityDefaultConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.GCTypeSubtypingProducts = true
	return compileWithFrontendFeatures(cfg, data, features)
}

func BenchmarkStagedGCTypeSubtypingRuntimeCallCast(b *testing.B) {
	pin := stagedGCTypeSubtypingProductPins[23]
	c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(b, pin))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("run"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedGCTypeSubtypingRuntimeTypedTable(b *testing.B) {
	pin := stagedGCTypeSubtypingTypedTablePin
	c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(b, pin))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("run"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedGCTypeSubtypingLinkProviderNullResult(b *testing.B) {
	c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(b, stagedGCTypeSubtypingLinkProviderPin))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("f2")
		if err != nil || len(got) != 1 || got[0] != 0 {
			b.Fatalf("f2 = %v, %v; want one null result", got, err)
		}
	}
}

func BenchmarkStagedGCTypeSubtypingFinalityLinkProviderEmpty(b *testing.B) {
	c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(b, stagedGCTypeSubtypingFinalityLinkProviderPin))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("f2")
		if err != nil || len(got) != 0 {
			b.Fatalf("f2 = %v, %v; want empty success", got, err)
		}
	}
}

func BenchmarkStagedGCTypeSubtypingStructLinkProviderEmpty(b *testing.B) {
	c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(b, stagedGCTypeSubtypingStructLinkProviderPin))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("g")
		if err != nil || len(got) != 0 {
			b.Fatalf("g = %v, %v; want empty success", got, err)
		}
	}
}

func BenchmarkStagedGCTypeSubtypingStructProjectionLinkProviderEmpty(b *testing.B) {
	c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(b, stagedGCTypeSubtypingStructProjectionLinkProviderPin))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("g")
		if err != nil || len(got) != 0 {
			b.Fatalf("g = %v, %v; want empty success", got, err)
		}
	}
}

func BenchmarkStagedGCTypeSubtypingStructMismatchLinkProviderEmpty(b *testing.B) {
	c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(b, stagedGCTypeSubtypingStructMismatchLinkProviderPin))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("g")
		if err != nil || len(got) != 0 {
			b.Fatalf("g = %v, %v; want empty success", got, err)
		}
	}
}

func BenchmarkStagedGCTypeSubtypingIndependentStructLinkProviderEmpty(b *testing.B) {
	c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(b, stagedGCTypeSubtypingIndependentStructLinkProviderPin))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("g")
		if err != nil || len(got) != 0 {
			b.Fatalf("g = %v, %v; want empty success", got, err)
		}
	}
}

func BenchmarkStagedGCTypeSubtypingExtendedProjectionLinkProviderEmpty(b *testing.B) {
	c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(b, stagedGCTypeSubtypingExtendedProjectionLinkProviderPin))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("h")
		if err != nil || len(got) != 0 {
			b.Fatalf("h = %v, %v; want empty success", got, err)
		}
	}
}

func BenchmarkStagedGCTypeSubtypingRuntimeFinalityRecovery(b *testing.B) {
	pin := stagedGCTypeSubtypingProductPins[24]
	c, err := compileStagedGCTypeSubtypingProductForTest(stagedGCTypeSubtypingProductData(b, pin))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.invokeLocal(0, nil); err != nil {
			b.Fatal(err)
		}
	}
}
