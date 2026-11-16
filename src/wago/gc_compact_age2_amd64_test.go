//go:build linux && amd64 && wago_gc_compact_helpers && wago_gc_compact_age2 && !wago_gc_compact_shadow && !wago_gc_compact_cohort

package wago

import (
	"context"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
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

func TestCompactAge2CloneIngress(t *testing.T) {
	source := newForeignCloneBoundaryInstance(t, foreignCloneEdgeModule(), GCConfig{ExperimentalCompactOffsets: true, ExperimentalCompactAge2: true, NurseryBytes: 256, ThroughputHeapBytes: 4096}, nil)
	target := newForeignCloneBoundaryInstance(t, foreignCloneEdgeModule(), GCConfig{ExperimentalCompactOffsets: true, ExperimentalCompactAge2: true, NurseryBytes: 256, ThroughputHeapBytes: 4096, CollectEveryAlloc: true}, nil)
	token := foreignCloneSourceToken(t, source)
	for i := 0; i < 12; i++ {
		clone, e := target.CloneGCRefFrom(source, token)
		if e != nil {
			t.Fatal(e)
		}
		if e = target.ReleaseGCRef(clone); e != nil {
			t.Fatal(e)
		}
	}
	root := cloneEdgeRawRoot(t, source, token)
	if e := source.gc.StructSet(root, 0, gc.RefValue(gc.I31New(1))); e != nil {
		t.Fatal(e)
	}
	if _, e := target.CloneGCRefFrom(source, token); e == nil {
		t.Fatal("wrong typed clone edge admitted")
	}
	if target.gc.Stats().MinorCollections == 0 {
		t.Fatal("clone did not exercise movement")
	}
}
