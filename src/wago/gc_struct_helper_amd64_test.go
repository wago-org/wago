//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/hex"
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

func TestStagedGCStructNumericSetProfiles(t *testing.T) {
	data := stagedGCStructMutationBytes(t)
	profiles := []struct {
		name string
		cfg  GCConfig
	}{
		{name: "throughput", cfg: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true, StressBarriers: true}},
		{name: "tiny", cfg: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true, StressBarriers: true}},
	}
	for _, tc := range profiles {
		t.Run(tc.name, func(t *testing.T) {
			c, err := compileStagedGCStruct(data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := instantiateCore(c, InstantiateOptions{GC: tc.cfg})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			for i := 0; i < 1000; i++ {
				want := uint64(uint32(i*17 - 33))
				got, err := in.Invoke("set", want)
				if err != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("set iteration %d = %v, %v; want [%d]", i, got, err, want)
				}
			}
		})
	}

	c, err := compileStagedGCStruct(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	allocs := testing.AllocsPerRun(1000, func() {
		if got, err := in.Invoke("set", 7); err != nil || len(got) != 1 || got[0] != 7 {
			panic("numeric struct.set failed")
		}
	})
	if allocs != 0 {
		t.Fatalf("numeric struct.set default steady-state allocations = %v, want 0", allocs)
	}
}

func TestStagedGCStructPackedGlobalExecution(t *testing.T) {
	data, err := hex.DecodeString(stagedGCStructPackedHex)
	if err != nil {
		t.Fatal(err)
	}
	profiles := []struct {
		name string
		cfg  GCConfig
	}{
		{name: "throughput", cfg: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}},
		{name: "tiny", cfg: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}},
	}
	actions := []struct {
		name string
		args []uint64
		want []uint64
	}{
		{name: "get_packed_g0_0", want: []uint64{0, 0}},
		{name: "get_packed_g1_0", want: []uint64{uint64(^uint32(1)), 254}},
		{name: "get_packed_g0_1", want: []uint64{1, 1}},
		{name: "get_packed_g1_1", want: []uint64{uint64(^uint32(0)), 255}},
		{name: "get_packed_g0_2", want: []uint64{2, 2}},
		{name: "get_packed_g1_2", want: []uint64{uint64(^uint32(1)), 65534}},
		{name: "get_packed_g0_3", want: []uint64{3, 3}},
		{name: "get_packed_g1_3", want: []uint64{uint64(^uint32(0)), 65535}},
		{name: "set_get_packed_g0_1", args: []uint64{257}, want: []uint64{1, 1}},
		{name: "set_get_packed_g0_3", args: []uint64{257}, want: []uint64{257, 257}},
	}
	for _, tc := range profiles {
		t.Run(tc.name, func(t *testing.T) {
			c, err := compileStagedGCStruct(data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			in, err := instantiateCore(c, InstantiateOptions{GC: tc.cfg})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if state := in.pluginState.Load(); state == nil || state.gcGlobalRootCount != 2 {
				t.Fatalf("packed GC root mapping = %#v", state)
			}
			for _, action := range actions {
				got, err := in.Invoke(action.name, action.args...)
				if err != nil || len(got) != len(action.want) {
					t.Fatalf("%s = %v, %v; want %v", action.name, got, err, action.want)
				}
				for i := range got {
					if got[i] != action.want[i] {
						t.Fatalf("%s result %d = %#x, want %#x", action.name, i, got[i], action.want[i])
					}
				}
			}
		})
	}

	c, err := compileStagedGCStruct(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := Capture(c, SnapshotOptions{}); err == nil || !strings.Contains(err.Error(), "WasmGC reference products") {
		t.Fatalf("packed snapshot capture = %v, want explicit WasmGC state gate", err)
	}
	blob, err := marshalCompiled(c)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Compiled
	if err := unmarshalCompiled(&loaded, blob[5:]); err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if loaded.usesGCStructHelpers() || (loaded.memoryDir != nil && len(loaded.memoryDir.gcStructGlobals) != 0) {
		t.Fatal("packed codec reload inherited live helper/global admission")
	}
	if _, err := instantiateCore(&loaded, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "required feature") {
		t.Fatalf("packed codec-loaded instantiate = %v, want required-feature rejection", err)
	}
	if in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 32, TinyBlockBytes: 16}}); err == nil || !strings.Contains(err.Error(), "tiny heap exhausted") {
		if in != nil {
			_ = in.Close()
		}
		t.Fatalf("packed tiny exhaustion = %v, want rooted second-allocation failure", err)
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

func BenchmarkStagedGCStructPackedSetGet(b *testing.B) {
	data, err := hex.DecodeString(stagedGCStructPackedHex)
	if err != nil {
		b.Fatal(err)
	}
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
		if _, err := in.Invoke("set_get_packed_g0_3", uint64(uint32(i))); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedGCStructPackedGet(b *testing.B) {
	data, err := hex.DecodeString(stagedGCStructPackedHex)
	if err != nil {
		b.Fatal(err)
	}
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
		if _, err := in.Invoke("get_packed_g1_3"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStagedGCStructNewDefaultSetGet(b *testing.B) {
	data := stagedGCStructMutationBytes(b)
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
		if _, err := in.Invoke("set", uint64(uint32(i))); err != nil {
			b.Fatal(err)
		}
	}
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
