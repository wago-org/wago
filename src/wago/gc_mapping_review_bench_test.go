//go:build linux && (amd64 || arm64) && !tinygo

package wago

import "testing"

func BenchmarkGCMappingLifecycle(b *testing.B) {
	for _, same := range []bool{true, false} {
		name := "across-domains"
		if same {
			name = "same-domain"
		}
		b.Run(name, func(b *testing.B) {
			c, err := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).Compile(gcLifecycleModule())
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			store := newReferenceStore(false)
			defer store.closeRuntime()
			if same {
				keeper, err := instantiateCore(c, InstantiateOptions{store: store})
				if err != nil {
					b.Fatal(err)
				}
				defer keeper.Close()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				in, err := instantiateCore(c, InstantiateOptions{store: store})
				if err != nil {
					b.Fatal(err)
				}
				if err := in.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
