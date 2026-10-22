//go:build linux && !riscv64 && !tinygo

package riscv64

func flushICache([]byte, int) error                                           { return ErrUnavailable }
func nativeCall(code, a0, a1, low, top, frame, trap uintptr) (uint64, uint64) { return 0, 2 }

const nativeAvailable = false
