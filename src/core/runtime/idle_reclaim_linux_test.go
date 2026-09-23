//go:build linux && (amd64 || arm64)

package runtime

import (
	"syscall"
	"testing"
	"unsafe"
)

func TestIdleEngineColdPagesAndHotContents(t *testing.T) {
	e, err := AcquireEngine()
	if err != nil {
		t.Fatal(err)
	}
	top, limit, size := e.StackTop(), e.StackLimit(), e.StackBytes()
	for i := range e.stack {
		e.stack[i] = 0x5a
	}
	if err := ReleaseEngine(e); err != nil {
		t.Fatal(err)
	}
	next, err := AcquireEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if next != e || next.StackTop() != top || next.StackLimit() != limit || next.StackBytes() != size {
		t.Fatal("stack capacity or identity changed")
	}
	cold := (len(next.stack) - idleNativeStackHotBytes) &^ (syscall.Getpagesize() - 1)
	for i, b := range next.stack {
		want := byte(0x5a)
		if i < cold {
			want = 0
		}
		if b != want {
			t.Fatalf("stack byte %d = %x, want %x", i, b, want)
		}
	}
}

func TestIdleEngineFailedReclaimCloses(t *testing.T) {
	e, err := AcquireEngine()
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mlock(e.stack[:syscall.Getpagesize()]); err != nil {
		e.Close()
		t.Skipf("mlock unavailable: %v", err)
	}
	// MADV_DONTNEED rejects locked pages. Close must unmap the failed candidate.
	if err := ReleaseEngine(e); err != nil {
		t.Fatal(err)
	}
	var resident [1]byte
	_, _, errno := syscall.Syscall(syscall.SYS_MINCORE, uintptr(unsafe.Pointer(&e.stack[0])), uintptr(syscall.Getpagesize()), uintptr(unsafe.Pointer(&resident[0])))
	if errno != syscall.ENOMEM {
		t.Fatalf("failed reclaim did not unmap stack: %v", errno)
	}
	next, err := AcquireEngine()
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if next == e {
		t.Fatal("reused closed Engine")
	}
}

func TestIdleArenaZeroReclaimedReuse(t *testing.T) {
	var previous *Arena
	for cycle := 0; cycle < 20; cycle++ {
		a, err := AcquireArena(64 << 10)
		if err != nil {
			t.Fatal(err)
		}
		if previous != nil && a != previous {
			t.Fatal("zero-reclaimed arena was not reused")
		}
		previous = a
		b := a.Alloc(len(a.mem))
		for i, v := range b {
			if v != 0 {
				t.Fatalf("cycle %d byte %d was not zero", cycle, i)
			}
			b[i] = 0xa5
		}
		if err := ReleaseArena(a); err != nil {
			t.Fatal(err)
		}
	}
	a, err := AcquireArena(64 << 10)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a != previous || a.Alloc(1)[0] != 0 {
		t.Fatal("reclaimed mapping did not return zeroed bytes")
	}
}

func TestIdleArenaFailedReclaimCloses(t *testing.T) {
	a, err := AcquireArena(64 << 10)
	if err != nil {
		t.Fatal(err)
	}
	b := a.Alloc(len(a.mem))
	for i := range b {
		b[i] = 0xa5
	}
	if err := syscall.Mlock(a.mem[:syscall.Getpagesize()]); err != nil {
		a.Close()
		t.Skipf("mlock unavailable: %v", err)
	}
	if err := ReleaseArena(a); err != nil {
		t.Fatal(err)
	}
	var resident [1]byte
	_, _, errno := syscall.Syscall(syscall.SYS_MINCORE, uintptr(unsafe.Pointer(&a.mem[0])), uintptr(syscall.Getpagesize()), uintptr(unsafe.Pointer(&resident[0])))
	if errno != syscall.ENOMEM {
		t.Fatalf("failed reclaim did not unmap Arena: %v", errno)
	}
	next, err := AcquireArena(64 << 10)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if next == a {
		t.Fatal("reused closed Arena")
	}
	for i, b := range next.Alloc(len(next.mem)) {
		if b != 0 {
			t.Fatalf("stale byte at %d", i)
		}
	}
}

func TestIdleOversizedEngineBypassesCache(t *testing.T) {
	e, err := AcquireEngineWithStackBytes(8 << 20)
	if err != nil {
		t.Fatal(err)
	}
	if err = ReleaseEngine(e); err != nil {
		t.Fatal(err)
	}
	var resident [1]byte
	_, _, errno := syscall.Syscall(syscall.SYS_MINCORE, uintptr(unsafe.Pointer(&e.stack[0])), uintptr(syscall.Getpagesize()), uintptr(unsafe.Pointer(&resident[0])))
	if errno != syscall.ENOMEM {
		t.Fatalf("oversized idle mapping was retained: %v", errno)
	}
}
