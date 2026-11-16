//go:build linux && amd64 && (wago_gc_helper_only || wago_gc_direct_reads || wago_gc_direct_alloc || wago_gc_direct_refstore || wago_gc_direct_trusted || wago_gc_direct_trusted_store || wago_gc_direct_scalar_arrays || wago_gc_direct_trusted_arrays || wago_gc_direct_array_alloc || wago_gc_direct_trusted_refstruct || wago_gc_direct_trusted_alloc || wago_gc_direct_fill)

package wago

import (
	"context"
	"os"
	"testing"
)

func TestDirectGCExactBenchmarksSmoke(t *testing.T) {
	for _, tc := range []struct {
		name    string
		n, want uint64
	}{
		{"gc-struct-allocation-access", 4096, 536739840},
		{"gc-array-allocation-access", 4096, 536739840},
		{"gc-array-fill", 4096, 536739840},
		{"gc-array-copy-checksum", 64, 221607936},
		{"gc-tree-survival", 32, 67551},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, e := os.ReadFile("../../bench/experiments/gc-v8/" + tc.name + ".wasm")
			if e != nil {
				t.Fatal(e)
			}
			c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			rt := NewRuntime()
			defer rt.Close()
			in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalDirectOffsets: true, NurseryBytes: 64 << 10, ThroughputHeapBytes: 8 << 20}})
			if e != nil {
				t.Fatal(e)
			}
			defer in.Close()
			if in.gc == nil || !in.gc.UsesDirectOffsets() {
				t.Fatal("direct backend not active")
			}
			calls := 3
			if tc.name == "gc-array-copy-checksum" {
				calls = 12
			}
			for i := 0; i < calls; i++ {
				got, e := in.Invoke("performance", tc.n)
				if e != nil || len(got) != 1 || got[0] != tc.want {
					t.Fatalf("got %v err %v want %d", got, e, tc.want)
				}
			}
			if in.gc.Stats().MinorCollections == 0 {
				t.Fatal("no real collections")
			}
		})
	}
}

func TestDirectGCInitializerStress(t *testing.T) {
	for _, kind := range []string{"struct", "uniform", "fixed"} {
		t.Run(kind, func(t *testing.T) {
			data := helperInitializerRewriteModule(kind)
			c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("inline", false), data)
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			rt := NewRuntime()
			defer rt.Close()
			in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalDirectOffsets: true, CollectEveryAlloc: true, NurseryBytes: 128, ThroughputHeapBytes: 64 << 10}})
			if e != nil {
				t.Fatal(e)
			}
			defer in.Close()
			for i := 0; i < 20; i++ {
				got, e := in.Invoke("run")
				want := uint64(37)
				if kind == "fixed" {
					want = 74
				}
				if e != nil || len(got) != 1 || got[0] != want {
					t.Fatal(got, e)
				}
			}
		})
	}
}

func TestDirectGCRejectsMissingAdmission(t *testing.T) {
	data := helperInitializerRewriteModule("struct")
	for _, mode := range []string{"receipt", "owner"} {
		t.Run(mode, func(t *testing.T) {
			c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			rt := NewRuntime()
			defer rt.Close()
			opts := InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalDirectOffsets: true}}
			if mode == "receipt" {
				c.gcHelperOnlyReceipt = false
				c.executionView().gcHelperOnlyReceipt = false
				c.gcDirectReadABIVersion = 0
				c.executionView().gcDirectReadABIVersion = 0
			} else {
				opts.store = nil
			}
			if in, e := Instantiate(c, opts); e == nil {
				in.Close()
				t.Fatal("unsupported direct execution admitted")
			}
		})
	}
}

func TestDirectGCRejectsHandleCollectorForNativeReads(t *testing.T) {
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), helperInitializerRewriteModule("struct"))
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if c.gcDirectReadABIReceipt() == 0 {
		t.Skip("native direct-read receipt only")
	}
	rt := NewRuntime()
	defer rt.Close()
	for _, cfg := range []GCConfig{{}, {Profile: GCProfileTiny}} {
		if in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: cfg}); e == nil {
			in.Close()
			t.Fatal("native offset artifact admitted to handle collector")
		}
	}
}

func TestDirectGCPublicRuntimeWithGC(t *testing.T) {
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
	defer rt.Close()
	mod, e := rt.Compile(helperInitializerRewriteModule("fixed"))
	if e != nil {
		t.Fatal(e)
	}
	defer mod.Close()
	in, e := rt.Instantiate(context.Background(), mod, WithGC(GCConfig{ExperimentalDirectOffsets: true, CollectEveryAlloc: true, ForceMajorEveryMinor: true, NurseryBytes: 128, ThroughputHeapBytes: 64 << 10}))
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	for i := 0; i < 5; i++ {
		got, e := in.Invoke("run")
		if e != nil || len(got) != 1 || got[0] != 74 {
			t.Fatal(got, e)
		}
	}
	if !in.gc.UsesDirectOffsets() || in.gc.Stats().FullCollections == 0 {
		t.Fatal("public API did not run moving collector")
	}
}

func TestDirectGCOptionDoesNotRequireHeapForScalarModules(t *testing.T) {
	data, e := os.ReadFile("../../bench/experiments/gc-v8/gc-access-scalar-baseline.wasm")
	if e != nil {
		t.Fatal(e)
	}
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
	defer rt.Close()
	mod, e := rt.Compile(data)
	if e != nil {
		t.Fatal(e)
	}
	defer mod.Close()
	in, e := rt.Instantiate(context.Background(), mod, WithGC(GCConfig{ExperimentalDirectOffsets: true}))
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	got, e := in.Invoke("performance", 4096)
	if e != nil || len(got) != 1 || got[0] != 536739840 {
		t.Fatal(got, e)
	}
	if in.gc != nil {
		t.Fatal("scalar module unexpectedly allocated collector")
	}
}
