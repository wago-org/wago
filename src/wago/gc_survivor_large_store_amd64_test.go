//go:build linux && amd64

package wago

import (
	"testing"

	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestGCNativeOldArrayStoreCardsLargeYoungChild(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeOldArrayReferenceStoreBytes())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{
		NurseryBytes: 1024, SurvivorBytes: 512, LargeObjectBytes: 16,
		ThroughputHeapBytes: 64 << 10, ThroughputPageBytes: 4096,
		StressBarriers: true, VerifyAfterCollect: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	array := corergc.Ref(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))
	if err := in.gc.ForcePromote(array); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Invoke("set_both"); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Invoke("clear_children"); err != nil {
		t.Fatal(err)
	}
	if in.gc.RememberedCount() != 1 {
		t.Fatalf("large-young native store remembered parents=%d, want 1", in.gc.RememberedCount())
	}
	if err := in.gc.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	value, err := in.gc.ArrayGet(array, 1)
	if err != nil || !value.Ref.IsObj() {
		t.Fatalf("large young child after native store/minor = %+v, %v", value, err)
	}
}
