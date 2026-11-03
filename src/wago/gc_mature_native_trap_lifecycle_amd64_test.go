//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"testing"
)

func TestContiguousEdenMatureRetainedTrapBatch(t *testing.T) {
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("gc-native-alloc", true).WithOptimization("dead-gc-new", false).WithOptimization("gc-lifetime-analyze", false), nativeRegionTrapModule())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	in, e := Instantiate(c, InstantiateOptions{GC: GCConfig{NurseryBytes: 65536, NurseryRegionShift: 6, VerifyAfterCollect: true}})
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	if _, e = in.Invoke("run", 0); e != nil {
		t.Fatal(e)
	}
	epoch := nativeRegionState(in, gc.NativeStructAllocEpochOffset)
	for i := 0; i < 10; i++ {
		before := in.gc.Stats().Allocations
		if _, e = in.Invoke("run", 1); e == nil {
			t.Fatal("missing trap")
		}
		if nativeRegionState(in, gc.NativeStructAllocCountOffset) == 0 || nativeRegionState(in, gc.NativeStructAllocEpochOffset) != epoch {
			t.Fatal("noncollecting trap changed valid direct batch")
		}
		if in.gc.Stats().Allocations != before+2 {
			t.Fatal("lost trap charges")
		}
		// Type growth preserves identities; generated code must load refreshed layouts.
		d, err := gc.NewStructDesc(gc.TypeID(100+i), nil)
		if err != nil {
			t.Fatal(err)
		}
		// Determine the next type ID from the native view, independent of module IDs.
		d.ID = gc.TypeID(in.gc.NativeView().SubtypeIntervalCount)
		if err = in.gc.AddTypes([]gc.TypeDesc{d}); err != nil {
			t.Fatal(err)
		}
		if got, err := in.Invoke("run", 0); err != nil || got[0] != 42 {
			t.Fatalf("after append %v %v", got, err)
		}
	}
	in.gc.CancelNativeAllocationBatch()
	if nativeRegionState(in, gc.NativeStructAllocCountOffset) != 0 || nativeRegionState(in, gc.NativeStructAllocEpochOffset) == epoch {
		t.Fatal("explicit cancellation retained batch")
	}
	if e = in.CollectGC(); e != nil {
		t.Fatal(e)
	}
	if got, err := in.Invoke("run", 0); err != nil || got[0] != 42 {
		t.Fatalf("after collection %v %v", got, err)
	}
}
