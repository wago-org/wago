//go:build linux && amd64 && wago_gc_compact_helpers && wago_gc_compact_age2 && !wago_gc_compact_shadow && !wago_gc_compact_cohort

package wago

import (
	"context"
	"testing"
)

func TestCompactAge2PublicRootsAndFlips(t *testing.T) {
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), compactPublicRootsModule())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	for _, every := range []bool{false, true} {
		rt := NewRuntime()
		in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalCompactOffsets: true, ExperimentalCompactAge2: true, NurseryBytes: 128, ThroughputHeapBytes: 4096, CollectEveryAlloc: every}})
		if e != nil {
			rt.Close()
			t.Fatal(e)
		}
		values, e := in.InvokeValues(context.Background(), "new", ValueI32(91))
		if e != nil {
			t.Fatal(e)
		}
		token := values[0].GCRef()
		if e = in.SetGlobalValue("root", values[0]); e != nil {
			t.Fatal(e)
		}
		initial := in.gc.CompactView().NurseryStart
		flipped := false
		for i := 0; i < 40; i++ {
			v, e := in.InvokeValues(context.Background(), "new", ValueI32(int32(i)))
			if e != nil {
				t.Fatal(e)
			}
			if e = in.ReleaseGCRef(v[0].GCRef()); e != nil {
				t.Fatal(e)
			}
			if in.gc.CompactView().NurseryStart != initial {
				flipped = true
			}
			got, e := in.Invoke("read")
			if e != nil || len(got) != 1 || got[0] != 91 {
				t.Fatal(got, e)
			}
		}
		if !flipped || in.gc.Stats().MinorCollections == 0 {
			t.Fatal("no observed native/helper flip", in.gc.Stats())
		}
		if e = in.SetGlobalValue("root", values[0]); e != nil {
			t.Fatal("original token after relocation", e)
		}
		if e = in.ReleaseGCRef(token); e != nil {
			t.Fatal(e)
		}
		in.Close()
		rt.Close()
	}
}
