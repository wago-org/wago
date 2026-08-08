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
