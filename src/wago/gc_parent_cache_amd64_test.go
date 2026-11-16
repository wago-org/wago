//go:build linux && amd64 && wago_gc_parent_cache && !tinygo && !wago_guardpage

package wago

import (
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"math"
	"testing"
)

func TestGCParentResolverCache(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeOldArrayReferenceStoreBytes())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	invoke := func(name string) {
		t.Helper()
		if _, err := in.Invoke(name); err != nil {
			t.Fatal(err)
		}
	}
	invoke("init")
	parent := gc.Ref(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))
	invoke("set_both")
	v := in.gcNativeView
	if v.ParentRef != uint32(parent) || v.ParentObject == 0 || v.ParentHandle == 0 {
		t.Fatalf("parent cache not populated: %+v", v)
	}
	for i := 0; i < 8; i++ {
		invoke("set_both")
	}
	before := in.gc.NativeView().RefreshGeneration
	if err := in.gc.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	if in.gc.NativeView().RefreshGeneration == before {
		t.Fatal("promotion did not invalidate certificate")
	}
	invoke("set_both")
	if err := in.gc.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	invoke("set_both")
	got, err := in.gc.ArrayGet(parent, 1)
	if err != nil || !got.Ref.IsObj() {
		t.Fatalf("stored child after collection: %+v %v", got, err)
	}
	// Saturated generation can never authorize a pointer, even if all keys match.
	in.gc.NativeView().RefreshGeneration = math.MaxUint64
	v.ParentGeneration = math.MaxUint64
	v.ParentObject = 1
	v.ParentHandle = 1
	invoke("set_both")
	if v.ParentObject == 1 || v.ParentHandle == 1 {
		t.Fatal("saturated cache was not re-resolved")
	}
}

func TestGCParentResolverCacheBounds(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeOldArrayReferenceStoreFixture(1, 1))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err = in.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	if _, err = in.Invoke("set_first"); err != nil {
		t.Fatal(err)
	}
	if _, err = in.Invoke("set_both"); err == nil {
		t.Fatal("cache hit skipped out-of-bounds trap")
	}
}
