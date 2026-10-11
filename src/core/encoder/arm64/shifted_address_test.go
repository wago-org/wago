package arm64

import (
	"bytes"
	"testing"
)

func TestShiftedAddressDisplacement(t *testing.T) {
	old := shiftedAddressDispEnabled
	defer func() { shiftedAddressDispEnabled = old }()
	shiftedAddressDispEnabled = true
	for _, tc := range []struct {
		disp int32
		want uint32
	}{
		{4096, 0x91400610}, {65536, 0x91404210}, {0xfff000, 0x917ffe10},
		{-4096, 0xd1400610}, {-65536, 0xd1404210}, {-0xfff000, 0xd17ffe10},
	} {
		var a Asm
		a.addDispX16(tc.disp)
		if got := word(&a); got != tc.want {
			t.Fatalf("disp=%d got=%08x want=%08x", tc.disp, got, tc.want)
		}
	}
	for _, disp := range []int32{0, 1, -1, 4095, -4095, 4097, -4097, 0x1000000, -0x1000000, 0x7fffffff, -0x80000000} {
		shiftedAddressDispEnabled = false
		var off Asm
		off.addDispX16(disp)
		shiftedAddressDispEnabled = true
		var on Asm
		on.addDispX16(disp)
		if !bytes.Equal(off.B, on.B) {
			t.Fatalf("unexpected admission: %d", disp)
		}
	}
}

func TestSplitIndexedAddressWidths(t *testing.T) {
	old := shiftedAddressDispEnabled
	defer func() { shiftedAddressDispEnabled = old }()
	for _, enabled := range []bool{false, true} {
		shiftedAddressDispEnabled = enabled
		for _, size := range []int{1, 2, 4, 8} {
			for _, disp := range []int32{4095, 4096, 4097, 0x40f8, 0x10000, 0x10008, 0xfff000, 0xffffff, 0x1000000, -4096, -0x40f8, -0x80000000} {
				for _, store := range []bool{false, true} {
					a := Asm{DisableLogicalMoveImmediate: true}
					if store {
						a.StoreIdx(X0, X1, X4, disp, size)
					} else {
						a.LoadIdx(X4, X0, X1, disp, size, false, true)
					}
					if got := displacementAddress(t, &a, X0); got != int64(disp) {
						t.Fatalf("enabled=%v size=%d disp=%x store=%v address=%x", enabled, size, disp, store, got)
					}
				}
			}
		}
	}
}

func TestSplitSignedLoadAddresses(t *testing.T) {
	old := shiftedAddressDispEnabled
	defer func() { shiftedAddressDispEnabled = old }()
	shiftedAddressDispEnabled = true
	for _, size := range []int{1, 2, 4, 8} {
		for _, wide := range []bool{false, true} {
			for _, disp := range []int32{0x40f8, 0x10000, 0xfff000, -4096} {
				a := Asm{DisableLogicalMoveImmediate: true}
				a.LoadIdx(X4, X0, X1, disp, size, true, wide)
				if got := displacementAddress(t, &a, X0); got != int64(disp) {
					t.Fatalf("size=%d wide=%v disp=%x address=%x", size, wide, disp, got)
				}
			}
		}
	}
}
