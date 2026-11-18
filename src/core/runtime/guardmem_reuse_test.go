//go:build (wago_guardpage || (darwin && arm64 && !tinygo && !wago_target_tinygo)) && (linux || darwin) && (amd64 || arm64)

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
	mem := unsafe.Slice((*byte)(unsafe.Pointer(page)), pageSize)
	if err := syscall.Mprotect(mem, syscall.PROT_READ|syscall.PROT_WRITE); err != nil {
		t.Fatalf("mprotect commit: %v", err)
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

func TestGuardedHostBytesTracksLogicalGrowth(t *testing.T) {
	const page = wasmPageBytes
	j, err := NewJobMemoryGuarded(page, 5*page)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	j.putU32(offActualLinMemByteSize, 5*page)
	j.putU64(offActualLinMemByteSize64, 5*page)
	if got := len(j.HostBytes()); got != 5*page {
		t.Fatalf("HostBytes length after logical growth = %d, want %d", got, 5*page)
	}
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
	j1.putU64(offActualLinMemByteSize64, uint64(3*page))
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
	if err := ReleaseJobMemory(j2); err != nil {
		t.Fatalf("release second: %v", err)
	}

	// Exercise both protection transitions of the retained first Wasm page. A
	// zero-page instance must not expose it; the following nonzero instance must
	// restore it as zero without remapping the reservation.
	j0, err := AcquireJobMemoryGuarded(0, page)
	if err != nil {
		t.Fatal(err)
	}
	if j0.reserveBase != base || len(j0.LinearMemory()) != 0 {
		t.Fatalf("zero-page reuse = base %#x len %d, want base %#x len 0", j0.reserveBase, len(j0.LinearMemory()), base)
	}
	if err := ReleaseJobMemory(j0); err != nil {
		t.Fatalf("release zero-page instance: %v", err)
	}
	j3, err := AcquireJobMemoryGuarded(page, page)
	if err != nil {
		t.Fatal(err)
	}
	if j3.reserveBase != base {
		t.Fatalf("post-zero reservation not reused: base %#x != %#x", j3.reserveBase, base)
	}
	for i, b := range j3.LinearMemory() {
		if b != 0 {
			t.Fatalf("post-zero initial memory byte %d = %#x, want 0", i, b)
		}
	}
	if err := j3.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
