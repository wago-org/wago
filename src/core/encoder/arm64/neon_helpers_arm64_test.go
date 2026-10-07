//go:build linux && arm64 && !tinygo

package arm64

import (
	"encoding/binary"
	"syscall"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/arm64spike"
)

func TestNeonPshufSExec(t *testing.T) {
	var input [16]byte
	for i, value := range []uint32{1, 2, 3, 4} {
		binary.LittleEndian.PutUint32(input[i*4:], value)
	}
	for _, tc := range []struct {
		name string
		imm  byte
		want [4]uint32
	}{
		{"reverse", 0x1b, [4]uint32{4, 3, 2, 1}},
		{"identity", 0xe4, [4]uint32{1, 2, 3, 4}},
		{"repeat", 0x00, [4]uint32{1, 1, 1, 1}},
		{"mixed", 0x86, [4]uint32{3, 2, 1, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output [16]byte
			var a Asm
			a.LdrQ(2, X0, 0)
			a.NeonPshufS(3, 2, tc.imm)
			a.StrQ(X1, 0, 3)
			a.Ret()
			code, err := arm64spike.MapExec(a.B)
			if err != nil {
				t.Fatal(err)
			}
			defer syscall.Munmap(code)
			arm64spike.Call2(uintptr(unsafe.Pointer(&code[0])), uintptr(unsafe.Pointer(&input[0])), uintptr(unsafe.Pointer(&output[0])))
			for i, want := range tc.want {
				if got := binary.LittleEndian.Uint32(output[i*4:]); got != want {
					t.Fatalf("lane %d = %d, want %d", i, got, want)
				}
			}
		})
	}
}

func TestNeonPshufSInPlaceExec(t *testing.T) {
	var input, output [16]byte
	for i, value := range []uint32{1, 2, 3, 4} {
		binary.LittleEndian.PutUint32(input[i*4:], value)
	}
	var a Asm
	a.LdrQ(2, X0, 0)
	a.NeonPshufSWithScratch(2, 2, 3, 0x1b)
	a.StrQ(X1, 0, 2)
	a.Ret()
	code, err := arm64spike.MapExec(a.B)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Munmap(code)
	arm64spike.Call2(uintptr(unsafe.Pointer(&code[0])), uintptr(unsafe.Pointer(&input[0])), uintptr(unsafe.Pointer(&output[0])))
	for i, want := range []uint32{4, 3, 2, 1} {
		if got := binary.LittleEndian.Uint32(output[i*4:]); got != want {
			t.Fatalf("lane %d = %d, want %d", i, got, want)
		}
	}
}

func TestNeonMovemaskBExec(t *testing.T) {
	input := [16]byte{0x80, 0, 0xff, 0, 0, 0x80, 0, 0, 0, 0, 0x80, 0, 0, 0, 0, 0xff}
	var output [16]byte
	var a Asm
	a.LdrQ(2, X0, 0)
	a.NeonMovemaskB(X0, 2)
	a.StrQ(X1, 0, 2)
	a.Ret()
	code, err := arm64spike.MapExec(a.B)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Munmap(code)
	got := arm64spike.Call2(uintptr(unsafe.Pointer(&code[0])), uintptr(unsafe.Pointer(&input[0])), uintptr(unsafe.Pointer(&output[0])))
	if want := uintptr(0x8425); got != want {
		t.Fatalf("movemask = %#x, want %#x", got, want)
	}
	if output != input {
		t.Fatalf("movemask changed source vector: %x, want %x", output, input)
	}
}
