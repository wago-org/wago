//go:build linux || darwin

package wago

import (
	"syscall"
	"testing"
)

func newGCNativeTestFrame(t testing.TB, size int) ([]byte, func()) {
	t.Helper()
	frame, err := syscall.Mmap(-1, 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		t.Fatalf("map native test frame: %v", err)
	}
	return frame, func() {
		t.Helper()
		if err := syscall.Munmap(frame); err != nil {
			t.Fatalf("unmap native test frame: %v", err)
		}
	}
}
