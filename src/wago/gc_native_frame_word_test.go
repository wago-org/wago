package wago

import (
	"encoding/binary"
	"runtime"
	"testing"
	"unsafe"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestNativeFrameRootRewriteClearsTypeEvidence(t *testing.T) {
	frame, release := newGCNativeTestFrame(t, 8)
	defer release()
	const tagged = uint64(2) | uint64(65535)<<32 | gc.NativeTypedRefKnown | uint64(7)<<gc.NativeTypedRefEpochShift
	binary.LittleEndian.PutUint64(frame[:], tagged)
	slot := nativeGCFrameRootSlot(uintptr(unsafe.Pointer(&frame[0])))
	if got := slot.GetRef(); got != 2 {
		t.Fatalf("tagged root = %d, want 2", got)
	}
	slot.SetRef(4)
	if got := binary.LittleEndian.Uint64(frame[:]); got != 4 {
		t.Fatalf("root rewrite retained stale type evidence: %#x, want 4", got)
	}
	runtime.KeepAlive(&frame)
}

func TestNativeFrameRootAdapterAllocations(t *testing.T) {
	frame, release := newGCNativeTestFrame(t, 8)
	defer release()
	address := uintptr(unsafe.Pointer(&frame[0]))
	allocs := testing.AllocsPerRun(100, func() {
		var slot gc.RootSlot = nativeGCFrameRootSlot(address)
		slot.SetRef(slot.GetRef())
	})
	if allocs != 0 {
		t.Fatalf("native frame root adapter allocated %g times, want 0", allocs)
	}
	runtime.KeepAlive(&frame)
}
