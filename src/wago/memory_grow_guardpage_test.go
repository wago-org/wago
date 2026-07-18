//go:build wago_guardpage && ((linux && (amd64 || arm64 || riscv64)) || (darwin && arm64))

package wago

import (
	"encoding/binary"
	"testing"
)

// TestMemoryBytesAfterGrowGuardPage is a regression for Memory.Bytes() using the
// grow-safe host accessor. Under guard-page bounds the Go-side j.mem slice stays
// capped at the initial commit while memory.grow commits pages in the reservation;
// Memory.Bytes() must reflect the grown logical size (previously it called
// CurrentBytes and panicked with "slice bounds out of range" after growth).
func TestMemoryBytesAfterGrowGuardPage(t *testing.T) {
	const page = 65536
	cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased)
	c, err := Compile(cfg, growMemModule())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	if got := len(in.Memory().Bytes()); got != page {
		t.Fatalf("initial Bytes() len = %d, want %d", got, page)
	}

	// Grow by 4 pages (1 -> 5). memory.grow returns the previous page count.
	r, err := in.Invoke("grow", I32(4))
	if err != nil {
		t.Fatalf("grow: %v", err)
	}
	if prev := AsI32(r[0]); prev != 1 {
		t.Fatalf("memory.grow returned %d, want previous count 1", prev)
	}

	// The whole point: Bytes() reflects the grown size and does not panic.
	if got := len(in.Memory().Bytes()); got != 5*page {
		t.Fatalf("after grow, Bytes() len = %d, want %d", got, 5*page)
	}

	// A byte written by wasm into a newly grown page is visible through Bytes().
	off := uint32(4 * page) // inside the 5th (freshly grown) page
	if _, err := in.Invoke("store", I32(int32(off)), I32(0xABCD)); err != nil {
		t.Fatalf("store into grown page: %v", err)
	}
	if got := binary.LittleEndian.Uint32(in.Memory().Bytes()[off:]); got != 0xABCD {
		t.Fatalf("grown page via Bytes() = %#x, want 0xABCD", got)
	}
}
