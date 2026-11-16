//go:build linux && amd64 && (wago_gc_direct_trusted || wago_gc_direct_trusted_store || wago_gc_direct_trusted_arrays || wago_gc_direct_trusted_refstruct || wago_gc_direct_trusted_alloc)

package wago

import (
	"context"
	"testing"
)

func TestTrustedDirectGlobalTokenIngress(t *testing.T) {
	cfg := GCConfig{ExperimentalDirectOffsets: true, CollectEveryAlloc: true, ForceMajorEveryMinor: true, NurseryBytes: 256, ThroughputHeapBytes: 64 << 10}
	rt := NewRuntime()
	defer rt.Close()
	makeInstance := func(data []byte) *Instance {
		c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { c.Close() })
		in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: cfg})
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { in.Close() })
		return in
	}
	in := makeInstance(directPublicRootsModule())
	values, e := in.InvokeValues(context.Background(), "new", ValueI32(91))
	if e != nil || len(values) != 1 {
		t.Fatal(values, e)
	}
	good := values[0]
	if e = in.SetGlobalValue("root", good); e != nil {
		t.Fatal(e)
	}
	checkRejected := func(value Value) {
		if e := in.SetGlobalValue("root", value); e == nil {
			t.Fatal("invalid token crossed global ingress")
		}
		got, e := in.Invoke("read")
		if e != nil || len(got) != 1 || got[0] != 91 {
			t.Fatal("rejection changed global", got, e)
		}
	}
	checkRejected(ValueGCRef(GCRef{token: good.GCRef().token ^ 0x4000000000000000}))
	other := makeInstance(foreignCloneEdgeModule())
	wrong, e := other.InvokeValues(context.Background(), "new")
	if e != nil || len(wrong) != 1 {
		t.Fatal(wrong, e)
	}
	checkRejected(wrong[0])
	other.ReleaseGCRef(wrong[0].GCRef())
	foreign := newForeignCloneBoundaryInstance(t, directPublicRootsModule(), cfg, nil)
	otherDomain, e := foreign.InvokeValues(context.Background(), "new", ValueI32(92))
	if e != nil || len(otherDomain) != 1 {
		t.Fatal(otherDomain, e)
	}
	checkRejected(otherDomain[0])
	foreign.ReleaseGCRef(otherDomain[0].GCRef())
	if e = in.ReleaseGCRef(good.GCRef()); e != nil {
		t.Fatal(e)
	}
	checkRejected(good)
}
