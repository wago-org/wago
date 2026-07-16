//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

func TestStagedGCStructGetDefaultCollectorProfiles(t *testing.T) {
	data := stagedGCStructGetOnlyBytes(t)
	if _, err := Compile(NewRuntimeConfig(), data); err == nil || !strings.Contains(err.Error(), "gc type") {
		t.Fatalf("public compile = %v, want closed GC gate", err)
	}

	profiles := []struct {
		name string
		cfg  GCConfig
	}{
		{name: "throughput-stress", cfg: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}},
		{name: "tiny-stress", cfg: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}},
	}
	for _, tc := range profiles {
		t.Run(tc.name, func(t *testing.T) {
			c, err := compileStagedGCStruct(data)
			if err != nil {
				t.Fatalf("staged compile: %v", err)
			}
			defer c.Close()
			if !c.usesGCStructHelpers() || c.stagedFeatures()&CoreFeatureGC == 0 {
				t.Fatalf("compiled helper sidecar/features = %v/%v", c.usesGCStructHelpers(), c.stagedFeatures())
			}
			in, err := instantiateCore(c, InstantiateOptions{GC: tc.cfg})
			if err != nil {
				t.Fatalf("instantiate: %v", err)
			}
			collector := in.gc
			if collector == nil {
				t.Fatal("collector-backed struct product has nil collector")
			}
			for i := 0; i < 1000; i++ {
				got, err := in.Invoke("get")
				if err != nil || len(got) != 1 || got[0] != 0 {
					t.Fatalf("get iteration %d = %v, %v; want [0]", i, got, err)
				}
			}
			stats := collector.Stats()
			if stats.Allocations != 1000 || stats.LiveObjects > 1 {
				t.Fatalf("collector stats = %+v, want 1000 allocations and at most one live object", stats)
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := collector.NewStructDefault(0); err == nil || !strings.Contains(err.Error(), "collector closed") {
				t.Fatalf("allocation after instance close = %v, want collector closed", err)
			}
		})
	}
}

func TestStagedGCStructGetAllocationFailureAndCodecGate(t *testing.T) {
	data := stagedGCStructGetOnlyBytes(t)
	c, err := compileStagedGCStruct(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 16, TinyBlockBytes: 16}})
	if err != nil {
		t.Fatalf("instantiate tiny exhausted product: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("get"); err == nil || !strings.Contains(err.Error(), "tiny heap exhausted") {
		t.Fatalf("tiny exhausted invocation = %v, want deterministic allocation failure", err)
	}
	if _, err := in.Invoke("get"); err == nil || !strings.Contains(err.Error(), "tiny heap exhausted") {
		t.Fatalf("tiny exhausted recovery invocation = %v, want repeatable failure", err)
	}

	if _, err := Capture(c, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "WasmGC reference products") {
		t.Fatalf("snapshot capture = %v, want explicit WasmGC state gate", err)
	}

	blob, err := marshalCompiled(c)
	if err != nil {
		t.Fatalf("marshal codec-v27: %v", err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatalf("private codec reload: %v", err)
	}
	defer loaded.Close()
	if loaded.usesGCStructHelpers() {
		t.Fatal("codec reload inherited live GC helper admission")
	}
	if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
		t.Fatalf("codec-loaded staged helper instantiate = %v, want fail-closed feature error", err)
	}
	var public Compiled
	if err := public.UnmarshalBinary(blob); err == nil || !strings.Contains(err.Error(), "unknown required feature bits") {
		t.Fatalf("public codec load = %v, want unsupported GC feature gate", err)
	}
}

func TestStagedGCStructHelperFootprint(t *testing.T) {
	if got := unsafe.Sizeof(compiledCodeCache{}); got != 64 {
		t.Fatalf("compiledCodeCache size = %d, want 64", got)
	}
	var _ gc.Ref = 0 // keep the compact reference representation explicit in this proof.
}

func BenchmarkStagedGCStructNewDefaultGet(b *testing.B) {
	data := stagedGCStructGetOnlyBytes(b)
	c, err := compileStagedGCStruct(data)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := in.Invoke("get"); err != nil {
			b.Fatal(err)
		}
	}
}
