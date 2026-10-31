package wago

import (
	"testing"
	"time"
)

// BenchmarkCompileSmallModule includes Close and reports time inside Compile
// separately so syscall/cleanup fixed costs are visible for tiny modules.
func BenchmarkCompileSmallModule(b *testing.B) {
	data := benchAddOneModule()
	cfg := NewRuntimeConfig().WithFunctionWorkers(1).WithBoundsChecks(BoundsChecksExplicit)
	var compileTime time.Duration
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		c, err := cfg.Compile(data)
		compileTime += time.Since(start)
		if err != nil {
			b.Fatal(err)
		}
		if err := c.Close(); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(compileTime.Nanoseconds())/float64(b.N), "compile-ns/op")
}
