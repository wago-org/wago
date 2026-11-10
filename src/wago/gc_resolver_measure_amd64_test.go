//go:build linux && amd64 && !tinygo

package wago

import (
	"crypto/sha256"
	"testing"
)

func TestGCRetainedResolverCodeIdentity(t *testing.T) {
	for _, kind := range []string{"phase", "phase1", "phase2", "reverse1", "reverse8"} {
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcRetainedResolverModule(2, 16, kind))
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("phase-code kind%s bytes%d sha256%x", kind, c.CodeSize(), sha256.Sum256(c.code))
		c.Close()
	}
}

func BenchmarkGCRetainedResolverCompilePhase(b *testing.B) {
	data := gcRetainedResolverModule(2, 256, "phase")
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		c, err := Compile(cfg, data)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(c.CodeSize()), "native-B")
		c.Close()
	}
}
