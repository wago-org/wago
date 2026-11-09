//go:build arm64

package arm64

import "testing"

func TestShiftedImmediateHintsPreserveCostlyCandidates(t *testing.T) {
	oldFilter, oldShift, oldOrdinary := shiftedImmediateHintFilterEnabled, shiftedAddSubImmediateEnabled, ordinaryShiftedImmediateEnabled
	t.Cleanup(func() {
		shiftedImmediateHintFilterEnabled, shiftedAddSubImmediateEnabled, ordinaryShiftedImmediateEnabled = oldFilter, oldShift, oldOrdinary
	})
	shiftedAddSubImmediateEnabled, ordinaryShiftedImmediateEnabled = true, true
	// Nested offset uses score above the outer loop's multiplier. These offsets
	// already have an immediate encoding and must not occupy its cache entries.
	body := []byte{0x03, 0x40}
	for range 3 {
		body = appendI32Const(body, -2048144789, 0x6c)
	}
	body = append(body, 0x03, 0x40)
	for range 4 {
		body = appendI32Const(body, 0x50000, 0x6a)
		body = appendI32Const(body, 0x10000, 0x6b)
	}
	body = append(body, 0x0b, 0x0b, 0x0b)
	for _, on := range []bool{false, true} {
		shiftedImmediateHintFilterEnabled = on
		h, err := scanBodyBytes(body, 0, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		want := uint8(3)
		if on {
			want = 1
		}
		if h.loopIntConstCount != want {
			t.Fatalf("filter%v count%d want%d", on, h.loopIntConstCount, want)
		}
		if on && (h.loopIntConst[0] != -2048144789 || h.loopIntConstTypes&3 != 1) {
			t.Fatalf("costly multiplier displaced: %+v", h.loopIntConst)
		}
	}
	for _, shift := range []bool{false, true} {
		for _, ordinary := range []bool{false, true} {
			shiftedAddSubImmediateEnabled, ordinaryShiftedImmediateEnabled = shift, ordinary
			shiftedImmediateHintFilterEnabled = true
			for _, op := range []byte{0x6a, 0x6b, 0x7c, 0x7d} {
				for _, v := range []int64{4096, -4096, 0xfff000, -0xfff000} {
					if got := loopIntConstNeedsRegister(op, v, 1); got == (shift && ordinary) {
						t.Fatalf("op%x value%d shift%v ordinary%v needed%v", op, v, shift, ordinary, got)
					}
				}
				for _, v := range []int64{4097, -4097, 0x1000000, -1 << 63} {
					if !loopIntConstNeedsRegister(op, v, 2) {
						t.Fatalf("unencodable value%d lost", v)
					}
				}
			}
		}
	}
}
