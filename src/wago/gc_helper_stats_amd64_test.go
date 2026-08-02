//go:build linux && amd64 && !tinygo && !wago_guardpage && wago_gcstats

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	corergc "github.com/wago-org/wago/src/core/runtime/gc"
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
