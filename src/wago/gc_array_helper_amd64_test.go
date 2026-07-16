//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"strings"
	"testing"
	"unsafe"
)

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

func TestStagedGCArrayHelperFootprint(t *testing.T) {
	if got := unsafe.Sizeof(compiledCodeCache{}); got != 64 {
		t.Fatalf("compiledCodeCache size = %d, want 64", got)
	}
}
