//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

func TestNativeGCViewVersionAndAllocationRefresh(t *testing.T) {
	compiled, err := compileStagedGCArray(stagedGCArrayNumericLocalBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{GC: GCConfig{DisableCollection: true, ThroughputHeapBytes: 2 << 20}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.gcNativeView == nil || in.jm.GCNativeViewPtr() != uintptr(unsafe.Pointer(in.gcNativeView)) {
		t.Fatalf("native GC instance view = %p, basedata = %#x", in.gcNativeView, in.jm.GCNativeViewPtr())
	}
	collectorView := in.gc.NativeView()
	if collectorView == nil || in.gcNativeView.Collector != uintptr(unsafe.Pointer(collectorView)) {
		t.Fatalf("native GC collector view = %p, instance pointer = %#x", collectorView, in.gcNativeView.Collector)
	}

	in.gcNativeView.Version = gc.NativeABIVersion + 1
	if _, err := in.Invoke("get", 1, 0); err == nil || !strings.Contains(err.Error(), "cast failure") {
		t.Fatalf("instance-view version mismatch = %v", err)
	}
	in.gcNativeView.Version = gc.NativeABIVersion
	collectorView.Version = gc.NativeABIVersion + 1
	if got, err := in.Invoke("get", 1, 0); err != nil || len(got) != 1 || got[0] != 0 {
		t.Fatalf("allocation-refreshed collector view = %v, %v", got, err)
	}
	if collectorView.Version != gc.NativeABIVersion {
		t.Fatalf("allocation did not republish collector ABI version: %d", collectorView.Version)
	}

	startGeneration := collectorView.RefreshGeneration
	startHandles := collectorView.Handles
	for i := 0; i < 2048; i++ {
		if got, err := in.Invoke("get", 1, 0); err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("direct get after allocation %d = %v, %v", i, got, err)
		}
	}
	if collectorView.HandleCount < 2049 || collectorView.RefreshGeneration < startGeneration+2048 {
		t.Fatalf("native view did not refresh per allocation: handles=%d generation=%d start=%d", collectorView.HandleCount, collectorView.RefreshGeneration, startGeneration)
	}
	if collectorView.Handles == 0 {
		t.Fatal("native view lost handle table")
	}
	// A relocating append is expected for this many handles, but correctness does
	// not depend on the allocator choosing a different address.
	_ = startHandles
}
