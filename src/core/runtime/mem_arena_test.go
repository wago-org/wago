//go:build linux && amd64

package runtime

import "testing"

// TestArenaAllocZeroingContract pins the difference between Alloc (zero-fills a
// reused arena's region) and AllocNoZero (leaves prior contents for the caller to
// overwrite). Instantiate relies on Alloc's zeroing for sparse table entries and
// on AllocNoZero for write-before-read control buffers.
func TestArenaAllocZeroingContract(t *testing.T) {
	a, err := NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.zeroOnAlloc = true // as a reused (AcquireArena) arena would be
	for i := range a.mem {
		a.mem[i] = 0xbb // stale data from a prior instance
	}

	z := a.Alloc(128)
	for i, b := range z {
		if b != 0 {
			t.Fatalf("Alloc byte %d = %#x, want 0 (reused arena must zero)", i, b)
		}
	}

	nz := a.AllocNoZero(128)
	sawStale := false
	for _, b := range nz {
		if b == 0xbb {
			sawStale = true
			break
		}
	}
	if !sawStale {
		t.Fatal("AllocNoZero zeroed its region; it must skip the clear so callers own initialization")
	}
}
