package shared

import "testing"

func TestGCDispatchReservedBits(t *testing.T) {
	const maximum = (AtomicWaitDispatchBit - 1) >> GCSafepointIDShift
	if GCSafepointIDMax != maximum {
		t.Errorf("maximum=%d, want %d", GCSafepointIDMax, maximum)
	}
	for _, id := range []uint32{0, 1, maximum} {
		payload, ok := EncodeGCDispatch(GCHelperIDMask, id)
		if !ok {
			t.Fatalf("valid safepoint %d rejected", id)
		}
		if payload&(uint32(7)<<29) != 0 {
			t.Fatalf("reserved tags set: %#x", payload)
		}
		helper, got := DecodeGCDispatch(payload)
		if helper != GCHelperIDMask || got != id {
			t.Fatalf("decode=(%d,%d)", helper, got)
		}
	}
	for _, id := range []uint32{maximum + 1, 1 << 22, 1 << 23, ^uint32(0)} {
		if payload, ok := EncodeGCDispatch(0, id); ok {
			t.Errorf("unsafe ID %d encoded as %#x", id, payload)
		}
	}
	if _, ok := EncodeGCDispatch(GCHelperIDMask+1, 0); ok {
		t.Fatal("oversized helper accepted")
	}
}
