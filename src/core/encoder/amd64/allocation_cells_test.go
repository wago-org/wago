//go:build amd64

package amd64

import (
	"bytes"
	"testing"
)

func TestAllocationCellsLoadIdxBytes(t *testing.T) {
	for dst := Reg(0); dst < 16; dst++ {
		for base := Reg(0); base < 16; base++ {
			for index := Reg(0); index < 16; index++ {
				for _, disp := range []int32{-2147483648, -129, -128, -1, 0, 1, 127, 128, 2147483647} {
					for _, size := range []int{1, 2, 4, 8} {
						for _, signed := range []bool{false, true} {
							for _, wide := range []bool{false, true} {
								var a, b Asm
								a.LoadIdx(dst, base, index, disp, size, signed, wide)
								b.oldAllocationLoadIdx(dst, base, index, disp, size, signed, wide)
								if !bytes.Equal(a.B, b.B) {
									t.Fatalf("dst/base/index=%d/%d/%d disp=%d size=%d signed=%v wide=%v %x != %x", dst, base, index, disp, size, signed, wide, a.B, b.B)
								}
							}
						}
					}
				}
			}
		}
	}
}
