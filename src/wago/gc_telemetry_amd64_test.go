//go:build linux && amd64 && !tinygo && !wago_guardpage && wago_gcstats

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcTelemetryModule() []byte {
	return gcDeadNewModule([][]byte{
		{0x5f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}, 1, []byte{
		0xfb, 0x01, 0x00, // struct.new_default 0
		0xd1, // ref.is_null
		0x0b,
	})
}

func gcStaticSiteExecutionModule(sites int) []byte {
	functions := make([][]byte, sites)
	exports := make([][]byte, sites)
	bodies := make([][]byte, sites)
	for i := 0; i < sites; i++ {
		functions[i] = wasmtest.ULEB(1)
		exports[i] = wasmtest.ExportEntry(fmt.Sprintf("site%d", i), 0, uint32(i))
		bodies[i] = wasmtest.Code([]byte{
			0xfb, 0x01, 0x00, // struct.new_default 0
			0xd1, // ref.is_null
			0x0b,
		})
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(functions...)),
		wasmtest.Section(7, wasmtest.Vec(exports...)),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)
}

func TestPublicGCCodeAndCollectorTelemetry(t *testing.T) {
	data := gcTelemetryModule()
	plain, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	if _, ok := plain.GCNativeCodeTelemetry(); ok {
		t.Fatal("plain compile unexpectedly retained GC code telemetry")
	}

	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithGCCodeTelemetry(true), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if compiled.CodeSize() != plain.CodeSize() {
		t.Fatalf("code telemetry changed native bytes: plain=%d measured=%d", plain.CodeSize(), compiled.CodeSize())
	}
	code, ok := compiled.GCNativeCodeTelemetry()
	if !ok || code.TotalBytes != uint64(compiled.CodeSize()) || code.AllocationBytes == 0 || code.HelperCallBytes == 0 || code.TrapStubBytes == 0 {
		t.Fatalf("GC code telemetry = %+v, enabled=%v", code, ok)
	}

	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{
		Telemetry:         new(GCTelemetry),
		CollectEveryAlloc: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for i := 0; i < 2; i++ {
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("run %d = %v, %v", i, got, err)
		}
	}
	snapshot, ok := instance.GCTelemetrySnapshot()
	if !ok || snapshot.Minor.Cycles != 2 || snapshot.Paths.GoAllocationPaths != 2 || snapshot.Paths.MinorCollections != 2 {
		t.Fatalf("collector telemetry = %+v, enabled=%v", snapshot, ok)
	}
	if !instance.ResetGCTelemetry() {
		t.Fatal("public telemetry reset failed")
	}
	snapshot, _ = instance.GCTelemetrySnapshot()
	if snapshot.Minor.Cycles != 0 || snapshot.Paths.GoAllocationPaths != 0 {
		t.Fatalf("reset collector telemetry = %+v", snapshot)
	}
}

func TestGCCollectorTelemetryInfersNativeFastAllocations(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), v128StructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Telemetry: new(GCTelemetry)}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	fn, err := instance.PrepareFunction("new_get")
	if err != nil {
		t.Fatal(err)
	}
	instance.ResetGCTelemetry()
	for i := uint64(0); i < 65; i++ {
		got, err := fn.Invoke(i, ^i)
		if err != nil || len(got) != 2 || got[0] != i || got[1] != ^i {
			t.Fatalf("constructor %d = %v, %v", i, got, err)
		}
	}
	snapshot, ok := instance.GCTelemetrySnapshot()
	// Two batch-boundary constructors refill through the rooted Go allocator;
	// the remaining 63 publish directly from generated code.
	if !ok || snapshot.Paths.NativeFastAllocations != 63 || snapshot.Paths.GoAllocationPaths != 2 || snapshot.Paths.HandleRefills != 2 || snapshot.Paths.ConditionalMediumPaths != 2 {
		t.Fatalf("native allocation paths = %+v, enabled=%v", snapshot.Paths, ok)
	}
}

// BenchmarkGCStaticSiteExecution compares one repeatedly executed allocation
// site with 4,096 sites each executed once per operation. Compilation,
// instantiation, and function lookup are outside the timer; every invocation
// contributes to a semantic checksum.
func BenchmarkGCStaticSiteExecution(b *testing.B) {
	const executions = 4096
	for _, sites := range []int{1, executions} {
		b.Run(fmt.Sprintf("sites=%d", sites), func(b *testing.B) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithGCCodeTelemetry(true), gcStaticSiteExecutionModule(sites))
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = compiled.Close() })
			instance, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{
				Telemetry:           new(GCTelemetry),
				NurseryBytes:        1 << 20,
				ThroughputHeapBytes: 64 << 20,
			}})
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = instance.Close() })
			functions := make([]*PreparedFunction, sites)
			for i := range functions {
				functions[i], err = instance.PrepareFunction(fmt.Sprintf("site%d", i))
				if err != nil {
					b.Fatal(err)
				}
			}
			instance.ResetGCTelemetry()
			var checksum uint64
			b.ReportAllocs()
			b.ReportMetric(executions, "executions/op")
			b.ReportMetric(float64(sites), "static-sites/op")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for j := 0; j < executions; j++ {
					fn := functions[0]
					if sites != 1 {
						fn = functions[j]
					}
					got, err := fn.Invoke()
					if err != nil || len(got) != 1 || got[0] != 0 {
						b.Fatalf("site %d = %v, %v", j, got, err)
					}
					checksum += got[0] + 1
				}
			}
			b.StopTimer()
			if checksum != uint64(b.N*executions) {
				b.Fatalf("semantic checksum = %d, want %d", checksum, b.N*executions)
			}
			snapshot, ok := instance.GCTelemetrySnapshot()
			if !ok {
				b.Fatal("collector telemetry disabled")
			}
			b.ReportMetric(float64(snapshot.Minor.Cycles)/float64(b.N), "minor-cycles/op")
			b.ReportMetric(float64(snapshot.Paths.NativeFastAllocations)/float64(b.N), "native-fast-allocs/op")
			b.ReportMetric(float64(snapshot.Paths.GoAllocationPaths)/float64(b.N), "go-alloc-paths/op")
		})
	}
}
