//go:build linux && arm64

package runtime

import (
	"syscall"
	"testing"
)

func TestReclaimSparseLinearPagesPreservesZeroOnReuse(t *testing.T) {
	const page = 4096
	if syscall.Getpagesize() != page {
		t.Skip("requires 4 KiB base pages")
	}
	j, err := NewJobMemory(arm64PMDPageBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	for _, i := range []int{0, page, 15*page + 19, 144 * page, len(j.mem) - 1} {
		j.mem[i] = 0x7f
	}
	if !reclaimSparseLinearPages(j.mem, arm64PMDPageBytes) {
		t.Fatal("private sparse mapping was not eligible")
	}
	for i, b := range j.mem {
		if b != 0 {
			t.Fatalf("byte %d remained dirty", i)
		}
	}
	for i := 0; i < 32*page; i += page {
		j.mem[i] = 0x47
	}
	if reclaimSparseLinearPages(j.mem, arm64PMDPageBytes) || j.mem[0] != 0x47 {
		t.Fatal("dense mapping did not defer to the ordinary discard")
	}
}
