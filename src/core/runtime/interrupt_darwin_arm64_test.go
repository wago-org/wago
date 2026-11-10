//go:build darwin && arm64 && !tinygo && !wago_target_tinygo

package runtime

import (
	"encoding/binary"
	"syscall"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestDarwinInterruptRejectsInvalidContextRegisters(t *testing.T) {
	task := machTaskSelf()
	for _, linMem := range []uintptr{0, 1, abi.TrapCellPtrOffset - 1, ^uintptr(0)} {
		if darwinTrapContextMatches(task, linMem, 123) {
			t.Fatalf("invalid context register %#x matched", linMem)
		}
	}
	mem, err := syscall.Mmap(-1, 0, syscall.Getpagesize(), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if mem != nil {
			_ = syscall.Munmap(mem)
		}
	}()
	const trap = uintptr(0x12345678)
	binary.LittleEndian.PutUint64(mem, uint64(trap))
	linMem := uintptr(unsafe.Pointer(&mem[0])) + abi.TrapCellPtrOffset
	if !darwinTrapContextMatches(task, linMem, trap) {
		t.Fatal("valid context did not match")
	}
	if darwinTrapContextMatches(task, linMem, trap+8) || darwinTrapContextMatches(task, linMem, 0) {
		t.Fatal("mismatched trap matched")
	}
	if err := syscall.Munmap(mem); err != nil {
		t.Fatal(err)
	}
	mem = nil
	if darwinTrapContextMatches(task, linMem, trap) {
		t.Fatal("retired mapping matched")
	}
}
