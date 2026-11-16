//go:build linux && amd64 && (wago_gc_compact_reads || wago_gc_compact_alloc)

package wago

import (
	"context"
	"testing"
)

func TestCompactNativeReceiptIsolation(t *testing.T) {
	for _, mode := range []string{"version", "mixed", "direct", "helper", "frozen"} {
		t.Run(mode, func(t *testing.T) {
			c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), helperInitializerRewriteModule("struct"))
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			rt := NewRuntime()
			defer rt.Close()
			v := c.executionView()
			switch mode {
			case "version":
				v.gcCompactNativeVersion = 2
			case "mixed":
				v.gcCompactHelperVersion = 1
			case "direct":
				v.gcDirectReadABIVersion = 1
			case "helper":
				v.gcHelperOnlyReceipt = true
			case "frozen":
				v.gcCompactNativeVersion = 0
			}
			if in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalCompactOffsets: true}}); e == nil {
				in.Close()
				t.Fatal("incompatible receipt admitted")
			}
		})
	}
}

func TestCompactNativeSharedRuntimeViews(t *testing.T) {
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)))
	defer rt.Close()
	m, e := rt.Compile(compactPublicRootsModule())
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	cfg := GCConfig{ExperimentalCompactOffsets: true, CollectEveryAlloc: true, ForceMajorEveryMinor: true, NurseryBytes: 128, ThroughputHeapBytes: 64 << 10}
	a, e := rt.Instantiate(context.Background(), m, WithGC(cfg))
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	b, e := rt.Instantiate(context.Background(), m, WithGC(cfg))
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	if a.gc != b.gc || a.compactState.view == nil || b.compactState.view == nil || a.compactState.view == b.compactState.view {
		t.Fatal("shared domain lost per-instance view ownership")
	}
	av, e := a.InvokeValues(context.Background(), "new", ValueI32(31))
	if e != nil {
		t.Fatal(e)
	}
	defer a.ReleaseGCRef(av[0].GCRef())
	if e = a.SetGlobalValue("root", av[0]); e != nil {
		t.Fatal(e)
	}
	bv, e := b.InvokeValues(context.Background(), "new", ValueI32(47))
	if e != nil {
		t.Fatal(e)
	}
	defer b.ReleaseGCRef(bv[0].GCRef())
	if e = b.SetGlobalValue("root", bv[0]); e != nil {
		t.Fatal(e)
	}
	for n := 0; n < 20; n++ {
		for _, in := range []*Instance{a, b} {
			if _, e = in.Invoke("tick"); e != nil {
				t.Fatal(e)
			}
		}
		for i, in := range []*Instance{a, b} {
			got, e := in.Invoke("read")
			want := []uint64{31, 47}[i]
			if e != nil || len(got) != 1 || got[0] != want {
				t.Fatal(i, got, e)
			}
			if in.jm.GCNativeViewPtr() != in.compactState.pointer() {
				t.Fatal("instance context switched views")
			}
		}
	}
	// Cross-store cloning is deliberately rejected within a shared Runtime;
	// same-domain typed values are the supported publication path.
	if _, e := b.CloneGCRefFrom(a, av[0].GCRef()); e == nil {
		t.Fatal("same-store clone accepted")
	}
	if e = b.SetGlobalValue("root", av[0]); e != nil {
		t.Fatal(e)
	}
	got, e := b.Invoke("read")
	if e != nil || len(got) != 1 || got[0] != 31 {
		t.Fatal(got, e)
	}
}
