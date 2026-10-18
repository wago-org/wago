//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"syscall"
	"testing"
	"unsafe"
)

func globalCleanupCompile(t *testing.T, module []byte, artifact bool) *Compiled {
	t.Helper()
	c, err := Compile(artifactTestConfig(artifact), module)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if !artifact {
		return c
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })
	return loaded
}

func globalCleanupMappingStatus(address uintptr) syscall.Errno {
	var resident [1]byte
	_, _, errno := syscall.Syscall(syscall.SYS_MINCORE, address, uintptr(syscall.Getpagesize()), uintptr(unsafe.Pointer(&resident[0])))
	return errno
}
