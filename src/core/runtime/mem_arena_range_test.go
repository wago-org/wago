//go:build ((linux || darwin || windows) && (amd64 || arm64)) || (linux && riscv64 && !tinygo && !wago_target_tinygo)

package runtime

import (
	"testing"
	"unsafe"
)

func TestArenaAllocatedRange(t *testing.T) {
	arena, err := NewArena(256)
	if err != nil {
		t.Fatal(err)
	}
	defer arena.Close()
	block := arena.Alloc(32)
	base := uintptr(unsafe.Pointer(unsafe.SliceData(block)))
	view := arena.AllocatedRange(base+8, 16)
	if len(view) != 16 || cap(view) != 16 || unsafe.SliceData(view) != &block[8] {
		t.Fatal("range lost allocated storage identity")
	}
	view[0] = 42
	if block[8] != 42 {
		t.Fatal("range copied arena storage")
	}
	for _, test := range []struct {
		address uintptr
		bytes   int
	}{{0, 1}, {base - 1, 1}, {base, -1}, {base, 0}, {base + 32, 1}, {base + 31, 2}, {^uintptr(0), 2}, {base, int(^uint(0) >> 1)}} {
		if arena.AllocatedRange(test.address, test.bytes) != nil {
			t.Fatal("unallocated range accepted", test)
		}
	}
	if n := testing.AllocsPerRun(100, func() {
		if len(arena.AllocatedRange(base+8, 16)) != 16 {
			panic("lost range")
		}
	}); n != 0 {
		t.Fatal("range lookup allocated", n)
	}
	if err := arena.Close(); err != nil {
		t.Fatal(err)
	}
	if arena.AllocatedRange(base, 1) != nil {
		t.Fatal("closed range accepted")
	}
}
