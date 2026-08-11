//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	gcOptimizationWorkloadEnv       = "WAGO_GC_OPT_WORKLOAD_WASM"
	gcOptimizationWorkloadExportEnv = "WAGO_GC_OPT_WORKLOAD_EXPORT"
	gcOptimizationWorkloadExpectEnv = "WAGO_GC_OPT_WORKLOAD_EXPECT"
)

// BenchmarkGCOptimizationWorkload is the retained real-payload A/B gate for
// structured facts, load forwarding, and late barriers. The fixture is external
// so large generated artifacts are not vendored. It must export a zero-argument
// entry (default `_start`, overridden by WAGO_GC_OPT_WORKLOAD_EXPORT); imports
// are satisfied by deterministic no-op hosts. WAGO_GC_OPT_WORKLOAD_EXPECT is a
// required comma-separated exact result vector (`none` for no results), so an A/B
// cannot pass merely because two wrong executions produced nonzero checksums.
func BenchmarkGCOptimizationWorkload(b *testing.B) {
	path := os.Getenv(gcOptimizationWorkloadEnv)
	if path == "" {
		b.Skipf("set %s to a standalone WasmGC workload exporting run", gcOptimizationWorkloadEnv)
	}
	export := os.Getenv(gcOptimizationWorkloadExportEnv)
	if export == "" {
		export = "_start"
	}
	expected, err := parseGCOptimizationWorkloadExpected(os.Getenv(gcOptimizationWorkloadExpectEnv))
	if err != nil {
		b.Fatalf("%s: %v", gcOptimizationWorkloadExpectEnv, err)
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
		if !slices.Equal(results, expected) {
			_ = instance.Close()
			b.Fatalf("%s results = %v, want %v", export, results, expected)
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

func parseGCOptimizationWorkloadExpected(value string) ([]uint64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("required exact result vector is unset (use none for no results)")
	}
	if strings.EqualFold(value, "none") || value == "[]" {
		return []uint64{}, nil
	}
	parts := strings.Split(value, ",")
	out := make([]uint64, len(parts))
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty result at position %d", i)
		}
		v, err := strconv.ParseUint(part, 0, 64)
		if err != nil {
			return nil, fmt.Errorf("result %d %q: %w", i, part, err)
		}
		out[i] = v
	}
	return out, nil
}

func TestParseGCOptimizationWorkloadExpected(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []uint64
	}{
		{in: "none", want: []uint64{}},
		{in: "[]", want: []uint64{}},
		{in: "7, 0x10", want: []uint64{7, 16}},
	} {
		got, err := parseGCOptimizationWorkloadExpected(tc.in)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Fatalf("parse %q = %v, %v; want %v", tc.in, got, err, tc.want)
		}
	}
	if _, err := parseGCOptimizationWorkloadExpected(""); err == nil {
		t.Fatal("empty expected vector accepted")
	}
}
