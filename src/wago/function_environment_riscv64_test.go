//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && (!wago_no_reference_types || !wago_no_gc || !wago_no_exception_handling)

package wago

import (
	"testing"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func managedCollectorTestInstance(t testing.TB, profile gc.Profile) *Instance {
	t.Helper()
	if !nativeCollectorFrameRootsBuilt {
		t.Skip("requires native collector support")
	}
	leaf, err := gc.NewStructDesc(0, []gc.StorageKind{gc.StorageI32})
	if err != nil {
		t.Fatal(err)
	}
	collector, err := gc.NewCollector(gc.Config{Profile: profile, NurseryBytes: 4096, TinyHeapBytes: 64 << 10, VerifyAfterCollect: true}, []gc.TypeDesc{leaf})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(collector.Close)
	return &Instance{c: &Compiled{}, gc: collector, refStore: newReferenceStore(true)}
}
