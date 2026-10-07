//go:build linux && arm64

package arm64

import (
	"encoding/binary"
	"syscall"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/arm64spike"
)

func TestNeonPshufSExec(t *testing.T) {
	var input, output [16]byte
	for i, value := range []uint32{1, 2, 3, 4} {
		binary.LittleEndian.PutUint32(input[i*4:], value)
	}
	var a Asm
	a.LdrQ(2, X0, 0)
	a.NeonPshufS(3, 2, 0x1b) // Reverse the four 32-bit lanes.
	a.StrQ(X1, 0, 3)
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
	var a Asm
	a.LdrQ(2, X0, 0)
	a.NeonMovemaskB(X0, 2)
	a.Ret()
	code, err := arm64spike.MapExec(a.B)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Munmap(code)
	got := arm64spike.Call2(uintptr(unsafe.Pointer(&code[0])), uintptr(unsafe.Pointer(&input[0])), 0)
	if want := uintptr(0x8425); got != want {
		t.Fatalf("movemask = %#x, want %#x", got, want)
	}
}
