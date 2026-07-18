//go:build linux && riscv64

package riscv64spike

import (
	"syscall"
	"testing"
	"unsafe"

	rv "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestRVVExecuteByteAdd(t *testing.T) {
	if !coreruntime.RISCV64HasRVV() {
		t.Skip("ratified RVV 1.0 is unavailable or disabled for this process")
	}
	var a rv.Asm
	if !a.Vsetivli(rv.Zero, 16, rv.VE8) || !a.Vle(1, rv.A0, rv.VE8) {
		t.Fatal("encode vector setup/load")
	}
	a.VaddVV(2, 1, 1)
	if !a.Vse(2, rv.A1, rv.VE8) {
		t.Fatal("encode vector store")
	}
	a.Ret()

	code, err := MapExec(a.B)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Munmap(code) }()

	in := [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 126, 255}
	var out [16]byte
	Call2(uintptr(unsafe.Pointer(&code[0])), uintptr(unsafe.Pointer(&in[0])), uintptr(unsafe.Pointer(&out[0])))
	for i := range out {
		want := in[i] + in[i]
		if out[i] != want {
			t.Fatalf("lane %d = %#x, want %#x", i, out[i], want)
		}
	}
}
