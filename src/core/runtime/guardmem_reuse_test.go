//go:build linux && amd64 && wago_guardpage

package runtime

import (
	"syscall"
	"testing"
	"unsafe"
)

// commitPage mprotects the page holding off (bytes into linear memory) RW,
// mimicking the fault handler committing a page after a memory.grow. It does not
// write, so a subsequent read observes whatever the kernel faults in.
func commitPage(t *testing.T, j *JobMemory, off uintptr) {
	t.Helper()
	addr := j.reserveBase + uintptr(j.linOff) + off
	page := addr &^ uintptr(pageSize-1)
	if _, _, errno := syscall.Syscall(syscall.SYS_MPROTECT, page, uintptr(pageSize),
		syscall.PROT_READ|syscall.PROT_WRITE); errno != 0 {
		t.Fatalf("mprotect commit: %v", errno)
	}
}

// pokeByte commits the page at off and writes v, dirtying a grown page.
func pokeByte(t *testing.T, j *JobMemory, off uintptr, v byte) {
	t.Helper()
	commitPage(t, j, off)
	*(*byte)(unsafe.Pointer(j.reserveBase + uintptr(j.linOff) + off)) = v
}

func peekByte(j *JobMemory, off uintptr) byte {
	return *(*byte)(unsafe.Pointer(j.reserveBase + uintptr(j.linOff) + off))
}

// TestGuardedJobMemoryReuse verifies the one-slot guard-page reuse cache: a
// released reservation is handed back by the next Acquire (same base, proving no
// re-mmap), and every page the previous instance dirtied — the initial region and
// a grown page beyond it — reads back zero, so instances cannot leak memory
// through a reused reservation.
func TestGuardedJobMemoryReuse(t *testing.T) {
	if err := InstallGuardTrapHandler(); err != nil {
		t.Fatal(err)
	}
	const page = wasmPageBytes

	j1, err := AcquireJobMemoryGuarded(page, 4*page)
	if err != nil {
		t.Fatal(err)
	}
	base := j1.reserveBase
	if base == 0 {
		t.Fatal("guarded memory has no reservation base")
	}
	// Dirty the whole initial page.
	lin := j1.LinearMemory()
	for i := range lin {
		lin[i] = 0xAB
	}
	// Simulate a memory.grow to 3 pages and dirty a byte in the third page, as a
	// faulting store would after growing the logical size.
	j1.putU32(offActualLinMemByteSize, uint32(3*page))
	pokeByte(t, j1, 2*page+123, 0xCD)

	// Release -> parked in the cache (decommitted + re-armed + zero-reclaimed).
	if err := ReleaseJobMemory(j1); err != nil {
		t.Fatalf("release: %v", err)
	}

	// Reacquire with a different initial/max: must reuse the SAME reservation.
	j2, err := AcquireJobMemoryGuarded(2*page, 8*page)
	if err != nil {
		t.Fatal(err)
	}
	if j2.reserveBase != base {
		t.Fatalf("reservation not reused: base %#x != %#x", j2.reserveBase, base)
	}
	// Size caches reflect the new instance, not the old one.
	if got := j2.curBytes(); got != 2*page {
		t.Fatalf("reused curBytes = %d, want %d", got, 2*page)
	}
	// The new initial region (2 pages) must be zero — no 0xAB leak.
	lin2 := j2.LinearMemory()
	for i, b := range lin2 {
		if b != 0 {
			t.Fatalf("reused initial memory byte %d = %#x, want 0", i, b)
		}
	}
	// The page the previous instance grew into and dirtied must read back zero
	// once recommitted (MADV_DONTNEED dropped it on release; mprotect alone would
	// keep the old 0xCD physical page and leak it).
	commitPage(t, j2, 2*page+123)
	if got := peekByte(j2, 2*page+123); got != 0 {
		t.Fatalf("recommitted grown page byte = %#x, want 0 (leak from previous instance)", got)
	}
	if err := j2.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
