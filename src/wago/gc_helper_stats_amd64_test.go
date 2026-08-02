//go:build linux && amd64 && !tinygo && !wago_guardpage && wago_gcstats

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
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
