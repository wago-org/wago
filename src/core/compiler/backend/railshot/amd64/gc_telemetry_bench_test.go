//go:build amd64

package amd64

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcStaticSiteModule(tb testing.TB, sites int) *wasm.Module {
	tb.Helper()
	functions := make([][]byte, sites)
	bodies := make([][]byte, sites)
	for i := 0; i < sites; i++ {
		functions[i] = wasmtest.ULEB(1)
		body := []byte{
			0xfb, 0x01, 0x00, // struct.new_default 0
			0xd1, // ref.is_null
			0x0b,
		}
		bodies[i] = wasmtest.Code(body)
	}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(functions...)),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		tb.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		tb.Fatal(err)
	}
	return m
}

// BenchmarkGCStaticSiteCompilation contrasts one hot static allocation site
// with thousands of sparsely used static sites. Runtime execution belongs in
// product benchmarks; this layer isolates compile B/op, allocations, JIT bytes,
// helper sequences, stubs, and root-map metadata per static site.
func BenchmarkGCStaticSiteCompilation(b *testing.B) {
	for _, sites := range []int{1, 4096} {
		b.Run(fmt.Sprintf("sites=%d", sites), func(b *testing.B) {
			m := gcStaticSiteModule(b, sites)
			b.ReportAllocs()
			b.ReportMetric(float64(sites), "static-sites/op")
			for i := 0; i < b.N; i++ {
				var stats ModuleStats
				compiled, err := CompileModuleWith(m, CompileOptions{Workers: 1, GCStructHelpers: true, Stats: &stats})
				if err != nil {
					b.Fatal(err)
				}
				var codeBytes, gcBytes int
				for _, function := range stats.Funcs {
					if function != nil {
						codeBytes += function.CodeBytes
						gcBytes += function.GCCodeBytes.Allocation
					}
				}
				if codeBytes == 0 || codeBytes > len(compiled.Code) || gcBytes == 0 {
					b.Fatalf("code/GC bytes = %d/%d, compiled=%d", codeBytes, gcBytes, len(compiled.Code))
				}
				b.ReportMetric(float64(len(compiled.Code)), "native-bytes/op")
				b.ReportMetric(float64(gcBytes), "gc-allocation-bytes/op")
			}
		})
	}
}

func gcResolverDensityModule(tb testing.TB, sites int) *wasm.Module {
	tb.Helper()
	body := []byte{0x00}
	for range sites {
		body = append(body, 0x20, 0x00, 0xfb, 0x02, 0x00, 0x00, 0x1a)
	}
	body = append(body, 0x41, 0x00, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			[]byte{0x60, 0x01, 0x64, 0x00, 0x01, 0x7f},
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		tb.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		tb.Fatal(err)
	}
	return m
}

func gcDistinctResolverModule(tb testing.TB, sites int) *wasm.Module {
	tb.Helper()
	body := []byte{0x00}
	funcType := []byte{0x60, byte(sites)}
	for i := 0; i < sites; i++ {
		funcType = append(funcType, 0x64, 0x00)
		body = append(body, 0x20, byte(i), 0xfb, 0x02, 0x00, 0x00)
	}
	funcType = append(funcType, 0x01, 0x7f)
	for i := 1; i < sites; i++ {
		body = append(body, 0x6a)
	}
	body = append(body, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 0x01, 0x7f, 0x00}, funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		tb.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		tb.Fatal(err)
	}
	return m
}

// BenchmarkGCResolverCodeSize permanently records the low/dense-site crossover
// for the module-owned compact-handle resolver and the bounded reuse certificate.
func BenchmarkGCResolverCodeSize(b *testing.B) {
	for _, sites := range []int{1, 8, 128} {
		m := gcResolverDensityModule(b, sites)
		for _, shared := range []bool{false, true} {
			b.Run(fmt.Sprintf("sites=%d/shared=%v", sites, shared), func(b *testing.B) {
				savedShared, savedReuse := gcSharedStubsEnabled, gcResolveReuseEnabled
				gcSharedStubsEnabled, gcResolveReuseEnabled = shared, false
				defer func() { gcSharedStubsEnabled, gcResolveReuseEnabled = savedShared, savedReuse }()
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					var stats ModuleStats
					compiled, err := CompileModuleWith(m, CompileOptions{Workers: 1, GCStructHelpers: true, Stats: &stats})
					if err != nil {
						b.Fatal(err)
					}
					b.ReportMetric(float64(len(compiled.Code)), "native-bytes/op")
					b.ReportMetric(float64(stats.GCSharedStubBytes), "shared-stub-bytes/op")
					b.ReportMetric(float64(stats.GCSharedStubCallSites), "shared-calls/op")
					if compiled.CodeImage != nil {
						_ = compiled.CodeImage.Close()
					}
				}
			})
		}
	}
	m := gcResolverDensityModule(b, 8)
	for _, reuse := range []bool{false, true} {
		b.Run(fmt.Sprintf("reuse=%v", reuse), func(b *testing.B) {
			savedShared, savedReuse := gcSharedStubsEnabled, gcResolveReuseEnabled
			gcSharedStubsEnabled, gcResolveReuseEnabled = true, reuse
			defer func() { gcSharedStubsEnabled, gcResolveReuseEnabled = savedShared, savedReuse }()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var stats ModuleStats
				compiled, err := CompileModuleWith(m, CompileOptions{Workers: 1, GCStructHelpers: true, Stats: &stats})
				if err != nil {
					b.Fatal(err)
				}
				function := stats.Funcs[0]
				b.ReportMetric(float64(len(compiled.Code)), "native-bytes/op")
				b.ReportMetric(float64(function.GCHandleResolutions), "resolutions/op")
				b.ReportMetric(float64(function.GCHandleResolutionReuse), "reuses/op")
				if compiled.CodeImage != nil {
					_ = compiled.CodeImage.Close()
				}
			}
		})
	}
	distinct := gcDistinctResolverModule(b, 8)
	b.Run("distinct-reuse", func(b *testing.B) {
		savedShared, savedReuse := gcSharedStubsEnabled, gcResolveReuseEnabled
		gcSharedStubsEnabled, gcResolveReuseEnabled = true, true
		defer func() { gcSharedStubsEnabled, gcResolveReuseEnabled = savedShared, savedReuse }()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var stats ModuleStats
			compiled, err := CompileModuleWith(distinct, CompileOptions{Workers: 1, GCStructHelpers: true, Stats: &stats})
			if err != nil {
				b.Fatal(err)
			}
			function := stats.Funcs[0]
			b.ReportMetric(float64(len(compiled.Code)), "native-bytes/op")
			b.ReportMetric(float64(stats.Compile.FunctionAttempts), "attempts/op")
			b.ReportMetric(float64(function.GCHandleResolutions), "resolutions/op")
			if compiled.CodeImage != nil {
				_ = compiled.CodeImage.Close()
			}
		}
	})
}

func TestGCStaticSiteTelemetrySmoke(t *testing.T) {
	for _, sites := range []int{1, 128} {
		m := gcStaticSiteModule(t, sites)
		var stats ModuleStats
		compiled, err := CompileModuleWith(m, CompileOptions{Workers: 1, GCStructHelpers: true, Stats: &stats})
		if err != nil {
			t.Fatal(err)
		}
		if len(stats.Funcs) != sites || len(compiled.Code) == 0 {
			t.Fatalf("sites %d produced funcs/code %d/%d", sites, len(stats.Funcs), len(compiled.Code))
		}
		for i, function := range stats.Funcs {
			if function == nil || function.GCCodeBytes.Allocation == 0 || function.GCCodeBytes.HelperCall == 0 {
				t.Fatalf("site %d telemetry = %+v", i, function)
			}
		}
	}
}
