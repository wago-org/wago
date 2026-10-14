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

func TestCmpImm32Mem(t *testing.T) {
	for _, tc := range []struct {
		base      Reg
		disp, imm int32
		want      string
	}{
		{R8, 160, 24, "4183b8a000000018"},
		{R8, 160, 520, "4181b8a000000008020000"},
		{RSP, 0, 127, "833c247f"},
		{RBP, 0, -1, "837d00ff"},
		{R12, -128, 256, "41817c248000010000"},
	} {
		var a Asm
		a.CmpImm32Mem(tc.base, tc.disp, tc.imm)
		if got := hex.EncodeToString(a.B); got != tc.want {
			t.Errorf("base=%d disp=%d imm=%d: %s, want%s", tc.base, tc.disp, tc.imm, got, tc.want)
		}
	}
}
