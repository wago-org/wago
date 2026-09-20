package artifactcache

import (
	"fmt"
	"github.com/wago-org/wago"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func BenchmarkCacheGeneration(b *testing.B) {
	for _, size := range []int{0, 1 << 20} {
		for _, telemetry := range []bool{false, true} {
			b.Run(fmt.Sprintf("padding%d/telemetry%t", size, telemetry), func(b *testing.B) {
				source := constantModule()
				if size > 0 {
					payload := make([]byte, size+2)
					payload[0] = 1
					payload[1] = 'x'
					source = append(source, wasmtest.Section(0, payload)...)
				}
				cfg := wago.NewRuntimeConfig().WithBoundsChecks(wago.BoundsChecksExplicit).WithGCCodeTelemetry(telemetry)
				rt := wago.NewRuntime(wago.WithRuntimeConfig(cfg))
				defer rt.Close()
				cache := Cache{Dir: b.TempDir(), Identity: []byte("benchmark-cache-generation")}
				module, err := cache.LoadOrCompile(source, cfg, rt)
				if err != nil {
					b.Fatal(err)
				}
				if err := module.Close(); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					module, err := cache.LoadOrCompile(source, cfg, rt)
					if err != nil {
						b.Fatal(err)
					}
					if err := module.Close(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
