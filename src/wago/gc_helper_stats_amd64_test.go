//go:build linux && amd64 && !tinygo && !wago_guardpage && wago_gcstats

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/wasmtest"
)

func TestGCExecutedHelperTransitionStats(t *testing.T) {
	data := gcDeadNewModule([][]byte{
		{0x5f, 0x00},
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	}, 1, []byte{
		0xfb, 0x01, 0x00, // struct.new_default 0
		0xd1, // ref.is_null
		0x0b,
	})
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	instance.SetGCHelperStatsTracking(true)
	defer instance.SetGCHelperStatsTracking(false)
	for want := uint64(1); want <= 2; want++ {
		got, err := instance.Invoke("run")
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("run %d = %v, %v", want, got, err)
		}
		stats := instance.GCHelperStats()
		if stats.Calls != want || stats.AllocationCalls != want || stats.MutationCalls != 0 {
			t.Fatalf("helper stats after run %d = %+v", want, stats)
		}
	}
}

func TestGCExecutedHelperStatsTrackBatchedNativeStructAllocation(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), v128StructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	fn, err := instance.PrepareFunction("new_get")
	if err != nil {
		t.Fatal(err)
	}
	instance.SetGCHelperStatsTracking(true)
	defer instance.SetGCHelperStatsTracking(false)
	for i := uint64(0); i < 65; i++ {
		got, err := fn.Invoke(i, ^i)
		if err != nil || len(got) != 2 || got[0] != i || got[1] != ^i {
			t.Fatalf("constructor %d = %#x, %v", i, got, err)
		}
	}
	stats := instance.GCHelperStats()
	if stats.Calls != 67 || stats.AllocationCalls != 2 || stats.StructInitializedAllocationCalls != 2 {
		t.Fatalf("batched allocation helper stats = %+v, want 65 v128 reads plus two 32-handle allocation refills", stats)
	}
}

func TestGCExecutedHelperStatsTrackOldStructBarrierFallback(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeOldStructReferenceStoreBytes())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	parent := corergc.Ref(uint32(readGlobalObject(instance.globalCells[0], instance.c.Globals[0].Type)))
	if err := instance.gc.ForcePromote(parent); err != nil {
		t.Fatal(err)
	}
	instance.SetGCHelperStatsTracking(true)
	defer instance.SetGCHelperStatsTracking(false)
	if _, err := instance.Invoke("set_self"); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Invoke("set_i31"); err != nil {
		t.Fatal(err)
	}
	if stats := instance.GCHelperStats(); stats.MutationCalls != 0 {
		t.Fatalf("old-to-old or immediate old-parent store used helper: %+v", stats)
	}
	if _, err := instance.Invoke("set_child"); err != nil {
		t.Fatal(err)
	}
	if stats := instance.GCHelperStats(); stats.MutationCalls != 1 || stats.AllocationCalls != 1 {
		t.Fatalf("first old-to-young store stats = %+v, want one allocation and one mutation fallback", stats)
	}
	if _, err := instance.Invoke("set_child"); err != nil {
		t.Fatal(err)
	}
	if stats := instance.GCHelperStats(); stats.MutationCalls != 1 || stats.AllocationCalls != 2 {
		t.Fatalf("remembered old-parent store stats = %+v, want second allocation without mutation fallback", stats)
	}
}

func TestGCExecutedHelperStatsTrackDistantArrayCardFallbacks(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeOldArrayReferenceStoreFixture(130, 129))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	array := corergc.Ref(uint32(readGlobalObject(instance.globalCells[0], instance.c.Globals[0].Type)))
	if err := instance.gc.ForcePromote(array); err != nil {
		t.Fatal(err)
	}
	instance.SetGCHelperStatsTracking(true)
	defer instance.SetGCHelperStatsTracking(false)
	if _, err := instance.Invoke("set_both"); err != nil {
		t.Fatal(err)
	}
	if stats := instance.GCHelperStats(); stats.MutationCalls != 2 || stats.AllocationCalls != 0 {
		t.Fatalf("distant old-array stores stats = %+v, want one fallback per fixed card", stats)
	}
	if _, err := instance.Invoke("set_first"); err != nil {
		t.Fatal(err)
	}
	if stats := instance.GCHelperStats(); stats.MutationCalls != 3 || stats.AllocationCalls != 0 {
		t.Fatalf("first repeated non-head store stats = %+v, want one move-to-front fallback", stats)
	}
	if _, err := instance.Invoke("set_first"); err != nil {
		t.Fatal(err)
	}
	if stats := instance.GCHelperStats(); stats.MutationCalls != 3 || stats.AllocationCalls != 0 {
		t.Fatalf("warmed moved card store stats = %+v, want native head-card reuse", stats)
	}
}

func TestGCExecutedHelperStatsTrackOldArrayCardFallback(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcNativeOldArrayReferenceStoreBytes())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	array := corergc.Ref(uint32(readGlobalObject(instance.globalCells[0], instance.c.Globals[0].Type)))
	if err := instance.gc.ForcePromote(array); err != nil {
		t.Fatal(err)
	}
	instance.SetGCHelperStatsTracking(true)
	defer instance.SetGCHelperStatsTracking(false)
	if _, err := instance.Invoke("set_both"); err != nil {
		t.Fatal(err)
	}
	if stats := instance.GCHelperStats(); stats.MutationCalls != 1 || stats.AllocationCalls != 0 {
		t.Fatalf("two old-array stores stats = %+v, want only the first card fallback", stats)
	}
}
