//go:build linux && amd64 && !tinygo

package wago

import "testing"

func BenchmarkStagedMultiMemoryLoads(b *testing.B) {
	b.Setenv("WAGO_BOUNDS", "explicit")
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.MultiMemory = true
	compiled, err := compileWithFrontendFeatures(cfg, localMultiMemoryExecModule(), features)
	if err != nil {
		b.Fatalf("compile: %v", err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		b.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("store1", I32(32), I32(7)); err != nil {
		b.Fatalf("initialize memory 1: %v", err)
	}
	for _, name := range []string{"load0", "load1"} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := in.Invoke(name, I32(32)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
