//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"testing"
)

func newCloneSingleObjectFixture(t testing.TB, config GCConfig) (*Instance, *Instance, GCRef) {
	t.Helper()
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	sourceRT, sourceHost, sourceModule, source := instantiateForeignCloneFixture(t, cfg, config)
	targetRT, targetHost, targetModule, target := instantiateForeignCloneFixture(t, cfg, config)
	t.Cleanup(func() {
		target.Close()
		targetModule.Close()
		targetHost.Close()
		targetRT.Close()
		source.Close()
		sourceModule.Close()
		sourceHost.Close()
		sourceRT.Close()
	})
	values, err := source.InvokeValues(context.Background(), "new")
	if err != nil || len(values) != 1 {
		t.Fatalf("source new = %v, %v", values, err)
	}
	token := values[0].GCRef()
	t.Cleanup(func() { source.ReleaseGCRef(token) })
	return source, target, token
}

func BenchmarkForeignCloneSingleObject(b *testing.B) {
	for _, profile := range []struct {
		name   string
		config GCConfig
	}{{"throughput", GCConfig{}}, {"tiny", GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16}}} {
		b.Run(profile.name, func(b *testing.B) {
			source, target, token := newCloneSingleObjectFixture(b, profile.config)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cloned, err := target.CloneGCRefFrom(source, token)
				if err != nil {
					b.Fatal(err)
				}
				if err := target.ReleaseGCRef(cloned); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
