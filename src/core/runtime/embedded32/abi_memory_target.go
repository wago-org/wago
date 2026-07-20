//go:build arm || riscv32

package embedded32

import "unsafe"

func memoryFromABI(base, size uint32) []byte {
	if base == 0 || size == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(base))), int(size))
}
