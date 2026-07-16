//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"encoding/hex"
	"math"
	"strings"
	"testing"
	"unsafe"
)

const stagedGCArrayNumericFixedHex = "0061736d0100000001b080808000095e7d005e7d01600001640060027f6400017d60017f017d60037f64017d017d60027f7d017d6001646a017f6000017f038880808000070203040506070806938080800001640000430000803f4300000040fb0800020b079d8080800004036e65770000036765740002077365745f6765740004036c656e00060afe8080800007908080800000430000803f4300000040fb0800020b89808080000020012000fb0b000b8880808000002000100010010b928080800000200120002002fb0e0120012000fb0b010b9b80808000002000430000803f43000000404300004040fb080103200110030b8680808000002000fb0f0b868080800000100010050b"

func TestStagedGCArrayNumericLocalProfiles(t *testing.T) {
	data := stagedGCArrayNumericLocalBytes(t)
	if _, err := Compile(NewRuntimeConfig(), data); err == nil || !strings.Contains(err.Error(), "gc type") {
		t.Fatalf("public compile = %v, want closed GC gate", err)
	}
	profiles := []struct {
		name string
		cfg  GCConfig
	}{
		{name: "throughput", cfg: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}},
		{name: "tiny", cfg: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}},
	}
	for _, tc := range profiles {
		t.Run(tc.name, func(t *testing.T) {
			c, err := compileStagedGCArray(data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if !c.usesGCArrayHelpers() || c.usesGCStructHelpers() || c.stagedGCArrayProduct() != stagedGCArrayProductNumericLocal {
				t.Fatalf("array helper/product sidecar = %v/%v/%v", c.usesGCArrayHelpers(), c.usesGCStructHelpers(), c.stagedGCArrayProduct())
			}
			in, err := instantiateCore(c, InstantiateOptions{GC: tc.cfg})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if got, err := in.Invoke("get", 3, 2); err != nil || len(got) != 1 || got[0] != 0 {
				t.Fatalf("get = %v, %v; want [0]", got, err)
			}
			if got, err := in.Invoke("set_get", 3, 1, 0x12345678); err != nil || len(got) != 1 || got[0] != 0x12345678 {
				t.Fatalf("set_get = %v, %v", got, err)
			}
			if got, err := in.Invoke("len", 7); err != nil || len(got) != 1 || got[0] != 7 {
				t.Fatalf("len = %v, %v; want [7]", got, err)
			}
			if _, err := in.Invoke("get", 3, 3); err == nil {
				t.Fatal("out-of-bounds get succeeded")
			}
			if _, err := in.Invoke("set_get", 3, 3, 1); err == nil {
				t.Fatal("out-of-bounds set succeeded")
			}
		})
	}
}

func TestStagedGCArrayNumericLocalTinyExhaustionAndAllocation(t *testing.T) {
	data := stagedGCArrayNumericLocalBytes(t)
	c, err := compileStagedGCArray(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 16, TinyBlockBytes: 16}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for i := 0; i < 2; i++ {
		if _, err := in.Invoke("len", 1); err == nil || !strings.Contains(err.Error(), "tiny heap exhausted") {
			t.Fatalf("tiny exhaustion %d = %v", i, err)
		}
	}

	fast, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer fast.Close()
	allocs := testing.AllocsPerRun(1000, func() {
		if got, err := fast.Invoke("set_get", 3, 1, 9); err != nil || len(got) != 1 || got[0] != 9 {
			panic("numeric array helper failed")
		}
	})
	if allocs != 0 {
		t.Fatalf("numeric array helper allocations = %v, want 0", allocs)
	}
}

func TestStagedGCArrayNumericFixedOfficialProduct(t *testing.T) {
	data, err := hex.DecodeString(stagedGCArrayNumericFixedHex)
	if err != nil {
		t.Fatal(err)
	}
	profiles := []struct {
		name string
		cfg  GCConfig
	}{
		{name: "throughput", cfg: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 96, VerifyAfterCollect: true}},
		{name: "tiny", cfg: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}},
	}
	for _, tc := range profiles {
		t.Run(tc.name, func(t *testing.T) {
			c, err := compileStagedGCArray(data)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if c.stagedGCArrayProduct() != stagedGCArrayProductNumericFixed || len(c.memoryDir.gcArrayGlobals) != 1 {
				t.Fatalf("fixed product/global sidecar = %v/%d", c.stagedGCArrayProduct(), len(c.memoryDir.gcArrayGlobals))
			}
			in, err := instantiateCore(c, InstantiateOptions{GC: tc.cfg})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if state := in.pluginState.Load(); state == nil || state.gcGlobalRootCount != 1 {
				t.Fatalf("fixed array root mapping = %#v", state)
			}
			if got, err := in.Invoke("get", 0); err != nil || len(got) != 1 || got[0] != uint64(math.Float32bits(1)) {
				t.Fatalf("get = %v, %v", got, err)
			}
			seven := uint64(math.Float32bits(7))
			if got, err := in.Invoke("set_get", 1, seven); err != nil || len(got) != 1 || got[0] != seven {
				t.Fatalf("set_get = %v, %v", got, err)
			}
			if got, err := in.Invoke("len"); err != nil || len(got) != 1 || got[0] != 2 {
				t.Fatalf("len = %v, %v", got, err)
			}
			if _, err := in.Invoke("get", 10); err == nil {
				t.Fatal("out-of-bounds fixed get succeeded")
			}
			raw, err := in.Invoke("new")
			if err != nil || len(raw) != 1 || raw[0] == 0 || raw[0]>>32 == 0 {
				t.Fatalf("new = %v, %v", raw, err)
			}
			if err := in.ReleaseGCRef(ValueOf(ValAnyRef, raw[0]).GCRef()); err != nil {
				t.Fatal(err)
			}
			values, err := in.Call(context.Background(), "new")
			if err != nil || len(values) != 1 || values[0].GCRef().IsNull() {
				t.Fatalf("Call new = %v, %v", values, err)
			}
			if err := in.ReleaseGCRef(values[0].GCRef()); err != nil {
				t.Fatal(err)
			}
		})
	}

	c, err := compileStagedGCArray(data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if in, err := instantiateCore(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 24, TinyBlockBytes: 8}}); err != nil {
		t.Fatal(err)
	} else {
		defer in.Close()
		if _, err := in.Invoke("new"); err == nil || !strings.Contains(err.Error(), "tiny heap exhausted") {
			t.Fatalf("fixed transient Tiny exhaustion = %v", err)
		}
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
	if loaded.usesGCArrayHelpers() || loaded.stagedGCArrayProduct() != 0 || (loaded.memoryDir != nil && len(loaded.memoryDir.gcArrayGlobals) != 0) {
		t.Fatal("codec reload inherited array helper/product/global admission")
	}
}

func TestStagedGCArrayHelperFootprint(t *testing.T) {
	if got := unsafe.Sizeof(compiledCodeCache{}); got != 64 {
		t.Fatalf("compiledCodeCache size = %d, want 64", got)
	}
	if got := unsafe.Sizeof(gcArrayGlobalInit{}); got != 48 {
		t.Fatalf("gcArrayGlobalInit size = %d, want 48", got)
	}
}

func BenchmarkStagedGCArrayNumericFixedSetGet(b *testing.B) {
	data, err := hex.DecodeString(stagedGCArrayNumericFixedHex)
	if err != nil {
		b.Fatal(err)
	}
	c, err := compileStagedGCArray(data)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	seven := uint64(math.Float32bits(7))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got, err := in.Invoke("set_get", 1, seven); err != nil || len(got) != 1 || got[0] != seven {
			b.Fatalf("set_get = %v, %v", got, err)
		}
	}
}

func BenchmarkStagedGCArrayNumericFixedPublicToken(b *testing.B) {
	data, err := hex.DecodeString(stagedGCArrayNumericFixedHex)
	if err != nil {
		b.Fatal(err)
	}
	c, err := compileStagedGCArray(data)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	in, err := instantiateCore(c, InstantiateOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer in.Close()
	warm, err := in.Invoke("new")
	if err != nil || len(warm) != 1 {
		b.Fatalf("warm new = %v, %v", warm, err)
	}
	if err := in.ReleaseGCRef(ValueOf(ValAnyRef, warm[0]).GCRef()); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := in.Invoke("new")
		if err != nil || len(got) != 1 {
			b.Fatalf("new = %v, %v", got, err)
		}
		if err := in.ReleaseGCRef(ValueOf(ValAnyRef, got[0]).GCRef()); err != nil {
			b.Fatal(err)
		}
	}
}
