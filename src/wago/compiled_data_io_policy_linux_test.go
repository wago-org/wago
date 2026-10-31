//go:build linux && !tinygo

package wago

import (
	"bytes"
	"syscall"
	"testing"
)

func TestCompactActiveDataDoesNotUseFilesystem(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	if _, err := syscall.InotifyAddWatch(fd, dir, syscall.IN_CREATE); err != nil {
		t.Fatal(err)
	}
	const segments = 1024
	source := bytes.Repeat([]byte{0x5a}, 16<<20)
	c := &Compiled{Data: make([]DataInit, segments)}
	for i := range c.Data {
		a, b := i*len(source)/segments, (i+1)*len(source)/segments
		c.Data[i] = DataInit{Offset: OffsetInit{Base: uint32(a)}, Bytes: source[a:b:b]}
	}
	snapshot := cloneCompiledExecutionMetadata(c)
	defer snapshot.Close()
	clear(source)
	if got := snapshot.activeDataAt(0).Bytes[0]; got != 0x5a {
		t.Fatalf("snapshot byte=%x", got)
	}
	var events [4096]byte
	n, err := syscall.Read(fd, events[:])
	if err != nil && err != syscall.EAGAIN {
		t.Fatal(err)
	}
	if n > 0 {
		t.Fatal("default active-data publication created a temporary file")
	}
}
