//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"reflect"
	"testing"

	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestGCNativeStructAllocPreparedAcrossCollections(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), v128StructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{StressNurseryBytes: 128, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("new_get")
	if err != nil {
		t.Fatal(err)
	}
	lo, hi := uint64(0x0706050403020100), uint64(0x0f0e0d0c0b0a0908)
	for i := 0; i < 2000; i++ {
		got, err := fn.Invoke(lo+uint64(i), hi^uint64(i))
		want := []uint64{lo + uint64(i), hi ^ uint64(i)}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("iteration %d = %#x, %v; want %#x", i, got, err, want)
		}
	}
	stats := in.gc.Stats()
	if stats.Allocations != 2001 || stats.MinorCollections == 0 {
		t.Fatalf("collector stats = %+v, want one global plus 2000 call allocations across minor collections", stats)
	}
}

func TestGCNativeStructAllocMalformedMetadataFallsBack(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), v128StructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.PrepareFunction("new_get")
	if err != nil {
		t.Fatal(err)
	}
	lo, hi := uint64(0x1111222233334444), uint64(0xaaaabbbbccccdddd)
	if got, err := fn.Invoke(lo, hi); err != nil || !reflect.DeepEqual(got, []uint64{lo, hi}) {
		t.Fatalf("initial constructor = %#x, %v", got, err)
	}
	view := in.gc.NativeView()
	state := view.StructAllocState
	view.StructAllocState = 0
	if got, err := fn.Invoke(lo+1, hi+1); err != nil || !reflect.DeepEqual(got, []uint64{lo + 1, hi + 1}) {
		t.Fatalf("nil allocation-state fallback = %#x, %v", got, err)
	}
	view.StructAllocState = state
	stride := view.HandleStride
	view.HandleStride++
	got, invokeErr := fn.Invoke(lo+2, hi+2)
	if nativeGCEntryValidationEnabled {
		if invokeErr == nil {
			t.Fatalf("wagodebug accepted mutated immutable handle stride: %#x", got)
		}
	} else if invokeErr != nil || !reflect.DeepEqual(got, []uint64{lo + 2, hi + 2}) {
		t.Fatalf("trusted immutable handle stride hot path = %#x, %v", got, invokeErr)
	}
	view.HandleStride = stride
}

func TestGCNativeStructAllocReferenceFieldsRemainRooted(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeOldStructReferenceStoreBytes())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{StressNurseryBytes: 256, StressBarriers: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	parent := corergc.Ref(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))
	if err := in.gc.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	fn, err := in.PrepareFunction("set_child")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2000; i++ {
		if _, err := fn.Invoke(); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}
	if err := in.gc.CollectMinor(nil); err != nil {
		t.Fatal(err)
	}
	value, err := in.gc.StructGet(parent, 0)
	if err != nil || !value.Ref.IsObj() {
		t.Fatalf("stored child after repeated native allocations = %+v, %v", value, err)
	}
}
