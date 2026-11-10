package shared

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestProvenAtomicAddressMinimumBoundary(t *testing.T) {
	for _, size := range []int{1, 2, 4, 8} {
		for _, pages := range []uint32{0, 1, 32769, 65536} {
			mt := wasm.MemType{Limits: wasm.Limits{Min: uint64(pages)}}
			for _, address := range []uint32{0, 1, 65528, 65535, 65536, 0x80000000, 0xfffffff8, 0xffffffff} {
				for _, offset := range []uint64{0, 1, 8, 0xffffffff, ^uint64(0)} {
					ea, got := ProvenAtomicAddress(mt, address, offset, size)
					// The independent interval form compares in words only after
					// rejecting overflow; this exercises the full 4 GiB boundary.
					start := uint64(address) + offset
					end := start + uint64(size)
					want := start >= offset && end >= start && end <= uint64(pages)*65536 && start%uint64(size) == 0
					if got != want || got && ea != start {
						t.Fatalf("pages=%d addr=%x off=%x size=%d: got %x/%t want %x/%t", pages, address, offset, size, ea, got, start, want)
					}
				}
			}
		}
	}
	if _, ok := ProvenAtomicAddress(wasm.MemType{Limits: wasm.Limits{Addr64: true, Min: 1}}, 0, 0, 8); ok {
		t.Fatal("memory64 address admitted")
	}
}
