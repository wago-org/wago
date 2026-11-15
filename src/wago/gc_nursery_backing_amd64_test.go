//go:build linux && amd64 && !tinygo && !wago_target_tinygo && !wago_guardpage

package wago

import (
	"context"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"runtime"
	"testing"
)

// This is an allocation witness, not a baseline semantics defect.
func TestGCExplicitNurseryAvoidsGoBackingAllocation(t *testing.T) {
	requireCompleteCore3Backend(t)
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeDefinedTypeModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{NurseryBytes: 8 << 20, SurvivorBytes: 4 << 20, ThroughputHeapBytes: 32 << 20, VerifyAfterCollect: true}})
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated >= 8<<20 {
		t.Fatalf("fixed nursery allocated %d Go bytes; want less than8MiB for a16MiB owned backing", allocated)
	}
	assertGCNativeDefinedTypeSemantics(t, in)
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
	assertGCNativeDefinedTypeSemantics(t, in)
	collector := in.gc
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if err := in.WaitClosed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !in.resourcesClosed || collector.NativeView().Spaces[gc.NativeSpaceNursery].Bytes != 0 {
		t.Fatal("completed close retained nursery storage")
	}
}
