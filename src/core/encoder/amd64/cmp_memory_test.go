package amd64

import (
	"encoding/hex"
	"testing"
)

func TestCmpImmIdx(t *testing.T) {
	for _, tc := range []struct {
		base, index Reg
		disp, imm   int32
		size        int
		want        string
	}{
		{RBX, RAX, 0, 255, 1, "803c03ff"},
		{R12, R13, 128, 0x5678, 2, "664381bc2c800000007856"},
		{R12, R13, 128, 65535, 2, "664383bc2c80000000ff"},
		{RBX, RAX, 0, 0x12345678, 4, "813c0378563412"},
		{RBX, RAX, 0, -1, 4, "833c03ff"},
		{R12, R13, 128, -1, 8, "4b83bc2c80000000ff"},
		{RBP, RAX, 0, 0, 1, "807c050000"},
	} {
		var a Asm
		a.CmpImmIdx(tc.base, tc.index, tc.disp, tc.imm, tc.size)
		if got := hex.EncodeToString(a.B); got != tc.want {
			t.Errorf("size=%d imm=%x: got %s want %s", tc.size, tc.imm, got, tc.want)
		}
	}
}
