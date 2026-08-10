//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"os"
	"testing"
)

const (
	gcOptimizationWorkloadEnv       = "WAGO_GC_OPT_WORKLOAD_WASM"
	gcOptimizationWorkloadExportEnv = "WAGO_GC_OPT_WORKLOAD_EXPORT"
)

// BenchmarkGCOptimizationWorkload is the retained real-payload A/B gate for
// structured facts, load forwarding, and late barriers. The fixture is external
// so large generated artifacts are not vendored. It must export a zero-argument
// entry (default `_start`, overridden by WAGO_GC_OPT_WORKLOAD_EXPORT); imports
// are satisfied by deterministic no-op hosts.
func BenchmarkGCOptimizationWorkload(b *testing.B) {
	path := os.Getenv(gcOptimizationWorkloadEnv)
	if path == "" {
		b.Skipf("set %s to a standalone WasmGC workload exporting run", gcOptimizationWorkloadEnv)
	}
	export := os.Getenv(gcOptimizationWorkloadExportEnv)
	if export == "" {
		export = "_start"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithBoundsChecks(BoundsChecksExplicit).WithGCCodeTelemetry(true), data)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = compiled.Close() })
	imports := make(Imports, len(compiled.Imports))
	for _, key := range compiled.Imports {
		imports[key] = HostFunc(func(HostModule, []uint64, []uint64) {})
	}
	if err := compiled.validateImportBindings(imports, nil); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	native, _ := compiled.GCNativeCodeTelemetry()
	var checksum uint64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		instance, err := Instantiate(compiled, InstantiateOptions{Imports: imports})
		if err != nil {
			b.Fatal(err)
		}
		results, err := instance.Invoke(export)
		if err != nil {
			_ = instance.Close()
			b.Fatal(err)
		}
		checksum++
		for _, value := range results {
			checksum = checksum*1099511628211 ^ value
		}
		if err := instance.Close(); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(len(compiled.code)), "linked-bytes")
	b.ReportMetric(float64(native.BarrierBytes), "gc-barrier-bytes")
	b.ReportMetric(float64(native.HelperCallBytes), "gc-helper-bytes")
	if checksum == 0 {
		b.Fatal("zero semantic checksum")
	}
}
