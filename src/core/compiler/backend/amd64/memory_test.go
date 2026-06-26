//go:build linux && amd64

package amd64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/runtime"
)

// fillMod / copyMod: a function taking (dst, b, n) and performing the bulk op.
func fillMod(t *testing.T) func(setup func([]byte), dst, val, n int32) ([]byte, error) {
	m := watToModule(t, `(module (memory 1) (func (export "f") (param i32 i32 i32)
		local.get 0 local.get 1 local.get 2 memory.fill))`)
	return func(setup func([]byte), dst, val, n int32) ([]byte, error) {
		_, mem, err := runMem(t, m, setup, dst, val, n)
		return mem, err
	}
}

func copyMod(t *testing.T) func(setup func([]byte), dst, src, n int32) ([]byte, error) {
	m := watToModule(t, `(module (memory 1) (func (export "f") (param i32 i32 i32)
		local.get 0 local.get 1 local.get 2 memory.copy))`)
	return func(setup func([]byte), dst, src, n int32) ([]byte, error) {
		_, mem, err := runMem(t, m, setup, dst, src, n)
		return mem, err
	}
}

func TestMemoryFill(t *testing.T) {
	fill := fillMod(t)
	mem, err := fill(nil, 10, 0xAB, 5)
	if err != nil {
		t.Fatal(err)
	}
	for i := 10; i < 15; i++ {
		if mem[i] != 0xAB {
			t.Fatalf("mem[%d] = %#x, want 0xAB", i, mem[i])
		}
	}
	if mem[9] != 0 || mem[15] != 0 {
		t.Fatalf("fill overran: mem[9]=%#x mem[15]=%#x", mem[9], mem[15])
	}

	// only the low byte of val is used
	mem, err = fill(nil, 0, 0x12FF, 3)
	if err != nil {
		t.Fatal(err)
	}
	if mem[0] != 0xFF || mem[1] != 0xFF || mem[2] != 0xFF {
		t.Fatalf("fill low-byte: %#x %#x %#x", mem[0], mem[1], mem[2])
	}

	// n == 0 is a no-op
	mem, err = fill(nil, 20, 0x77, 0)
	if err != nil {
		t.Fatal(err)
	}
	if mem[20] != 0 {
		t.Fatalf("n=0 fill wrote mem[20]=%#x", mem[20])
	}
}

func TestMemoryCopyDisjoint(t *testing.T) {
	copyfn := copyMod(t)
	mem, err := copyfn(func(l []byte) { copy(l, []byte{1, 2, 3, 4}) }, 100, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []byte{1, 2, 3, 4} {
		if mem[100+i] != want {
			t.Fatalf("mem[%d] = %d, want %d", 100+i, mem[100+i], want)
		}
	}
}

// TestMemoryCopyOverlap verifies memmove semantics in both directions — a plain
// forward rep movsb would corrupt the dst>src case.
func TestMemoryCopyOverlap(t *testing.T) {
	copyfn := copyMod(t)
	init := func(l []byte) { copy(l, []byte{1, 2, 3, 4, 5}) }

	// dst > src, overlapping: copy [0..3) -> [2..5). memmove => [1,2,1,2,3].
	mem, err := copyfn(init, 2, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := mem[:5]; got[2] != 1 || got[3] != 2 || got[4] != 3 {
		t.Fatalf("dst>src overlap: got %v, want [1 2 1 2 3]", got)
	}

	// dst < src, overlapping: copy [2..5) -> [0..3). memmove => [3,4,5,4,5].
	mem, err = copyfn(init, 0, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := mem[:5]; got[0] != 3 || got[1] != 4 || got[2] != 5 {
		t.Fatalf("dst<src overlap: got %v, want [3 4 5 4 5]", got)
	}
}

func TestMemoryBulkTraps(t *testing.T) {
	fill := fillMod(t)
	copyfn := copyMod(t)
	const size = 1 << 16

	isOOB := func(err error) bool {
		var te *runtime.TrapError
		return errors.As(err, &te) && te.Code == runtime.TrapLinMemOutOfBounds
	}

	if _, err := fill(nil, size-5, 0xFF, 10); !isOOB(err) { // dst+n > size
		t.Fatalf("fill OOB: got %v", err)
	}
	if _, err := copyfn(nil, size-5, 0, 10); !isOOB(err) { // dst+n > size
		t.Fatalf("copy dst OOB: got %v", err)
	}
	if _, err := copyfn(nil, 0, size-5, 10); !isOOB(err) { // src+n > size
		t.Fatalf("copy src OOB: got %v", err)
	}

	// Boundary: writing exactly up to the end is fine; one past traps.
	if _, err := fill(nil, size-4, 0xFF, 4); err != nil {
		t.Fatalf("fill to end should not trap: %v", err)
	}
	// dst == size with n == 0 is in bounds (nothing written).
	if _, err := fill(nil, size, 0xFF, 0); err != nil {
		t.Fatalf("fill dst==size n==0 should not trap: %v", err)
	}
	if _, err := fill(nil, size, 0xFF, 1); !isOOB(err) {
		t.Fatalf("fill dst==size n==1 should trap: got %v", err)
	}
}
