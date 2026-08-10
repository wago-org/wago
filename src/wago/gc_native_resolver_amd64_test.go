//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func gcNativeResolverReuseModule() []byte {
	body := []byte{
		0x01, 0x01, 0x63, 0x00, // one nullable (ref 0) local
		0xfb, 0x01, 0x00, 0x21, 0x00, // struct.new_default 0; local.set 0
	}
	for i := 0; i < 8; i++ {
		body = append(body, 0x20, 0x00, 0xfb, 0x02, 0x00, 0x00)
	}
	for i := 1; i < 8; i++ {
		body = append(body, 0x6a) // sum all zero-valued fields after the safe reuse region
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func gcNativeResolverSharedModule() []byte {
	body := []byte{
		0x00,
		0xfb, 0x01, 0x00, // struct.new_default 0
		0xfb, 0x02, 0x00, 0x00, // struct.get 0 0
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 0x01, 0x7f, 0x00},
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1), wasmtest.ULEB(1))),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(body))), body...),
			append(wasmtest.ULEB(uint32(len(body))), body...),
		)),
	)
}

func TestGCNativeResolverTelemetryAttributesModuleStub(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithGCCodeTelemetry(true), gcNativeResolverSharedModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	telemetry, ok := compiled.GCNativeCodeTelemetry()
	if !ok || telemetry.SharedStubBytes == 0 || telemetry.HandleResolutionBytes == 0 || telemetry.TotalBytes != uint64(compiled.CodeSize()) {
		t.Fatalf("native resolver telemetry = %+v, enabled=%v", telemetry, ok)
	}
}

func TestGCNativeResolverReuseExecution(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeResolverReuseModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{GC: GCConfig{DisableCollection: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 1000; i++ {
		got, err := in.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("iteration %d = %v, %v", i, got, err)
		}
	}
}

// BenchmarkGCNativeResolverReuse is intended for process-level differential
// runs with WAGO_AMD64_NO_GC_RESOLVE_REUSE and WAGO_AMD64_NO_GC_SHARED_STUBS.
func BenchmarkGCNativeResolverReuse(b *testing.B) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeResolverReuseModule())
	if err != nil {
		b.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{GC: GCConfig{DisableCollection: true, ThroughputHeapBytes: 64 << 20}})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 0 {
			b.Fatalf("run = %v, %v", got, err)
		}
	}
}
