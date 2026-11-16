//go:build linux && amd64 && wago_gc_compact_helpers && wago_gc_compact_alloc && wago_gc_compact_age2 && !wago_gc_compact_shadow && !wago_gc_compact_cohort && !wagodebug

package wago

import (
	"context"
	"encoding/binary"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"testing"
)

func TestCompactAge2NativeConstructorAfterFlip(t *testing.T) {
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), compactPublicRootsModule())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	rt := NewRuntime()
	defer rt.Close()
	in, e := Instantiate(c, InstantiateOptions{store: rt.refStore, GC: GCConfig{ExperimentalCompactOffsets: true, ExperimentalCompactAge2: true, NurseryBytes: 128, ThroughputHeapBytes: 1024}})
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	index := in.ctrl[coreruntime.HostCtrlImportIndexOffset:]
	first := in.gc.CompactView().NurseryStart
	for i := 0; i < 4; i++ {
		const sentinel = 0x81234567
		binary.LittleEndian.PutUint32(index, sentinel)
		v, e := in.InvokeValues(context.Background(), "new", ValueI32(int32(i)))
		if e != nil {
			t.Fatal(e)
		}
		if binary.LittleEndian.Uint32(index) != sentinel {
			t.Fatal("roomy post-flip allocation used helper", i)
		}
		if e = in.SetGlobalValue("root", v[0]); e != nil {
			t.Fatal(e)
		}
		if e = in.gc.CollectMinor(gc.EmptyRoots{}); e != nil {
			t.Fatal(e)
		}
		want := first
		if i%2 == 0 {
			want = first + 64
		}
		if in.gc.CompactView().NurseryStart != want {
			t.Fatal("no alternating flip")
		}
		got, e := in.Invoke("read")
		if e != nil || len(got) != 1 || got[0] != uint64(i) {
			t.Fatal(got, e)
		}
		if e = in.ReleaseGCRef(v[0].GCRef()); e != nil {
			t.Fatal(e)
		}
	}
	if in.gc.Stats().Allocations != 4 {
		t.Fatal(in.gc.Stats())
	}
}
