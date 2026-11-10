//go:build (linux || darwin || windows) && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func store8CompareModule(t *testing.T) *wasm.Module {
	t.Helper()
	// mem[$addr] = byte($x <s $y); return mem[$addr]
	return modMem(t, 1,
		[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32},
		[]wasm.ValType{wasm.I32},
		[]byte{
			0x00,
			0x20, 0x00,
			0x20, 0x01,
			0x20, 0x02,
			0x48,
			0x3a, 0x00, 0x00,
			0x20, 0x00,
			0x2d, 0x00, 0x00,
			0x0b,
		})
}

func TestStore8FlagsSink(t *testing.T) {
	m := store8CompareModule(t)
	for _, guard := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit", true: "guard"}[guard], func(t *testing.T) {
			s := compileWithStats(t, m, guard).Funcs[0]
			if got := s.Peephole["store8-flags"]; got != 1 {
				t.Fatalf("store8-flags = %d, want 1 (all: %v)", got, s.Peephole)
			}
			if got := s.Peephole["compare-setcc"]; got != 0 {
				t.Fatalf("compare-setcc = %d, want 0 after byte-store sink", got)
			}
			if got := s.Peephole["cmp-branch-fuse"]; got != 0 {
				t.Fatalf("cmp-branch-fuse = %d, want 0 after event reclassification", got)
			}
		})
	}

	for _, tc := range []struct {
		x, y uint64
		want byte
	}{
		{x: 3, y: 5, want: 1},
		{x: 5, y: 3, want: 0},
		{x: uint64(uint32(0x80000000)), y: 0, want: 1},
	} {
		_, mem, err := runMemAmd64(t, m, nil, 37, tc.x, tc.y)
		if err != nil {
			t.Fatalf("run(%#x, %#x): %v", tc.x, tc.y, err)
		}
		if got := mem[37]; got != tc.want {
			t.Fatalf("run(%#x, %#x) stored %d, want %d", tc.x, tc.y, got, tc.want)
		}
	}
}

func TestStore8FlagsKillSwitch(t *testing.T) {
	defer func(prev bool) { store8FlagsEnabled = prev }(store8FlagsEnabled)
	store8FlagsEnabled = false
	s := compileWithStats(t, store8CompareModule(t), false).Funcs[0]
	if got := s.Peephole["store8-flags"]; got != 0 {
		t.Fatalf("store8-flags = %d, want 0 with kill switch", got)
	}
	if got := s.Peephole["compare-setcc"]; got != 1 {
		t.Fatalf("compare-setcc = %d, want 1 with kill switch", got)
	}
}

func TestStore8FlagsNestedFixedRegisterPressure(t *testing.T) {
	// Four pending loads occupy the remaining freely allocatable registers,
	// making the byte-result scratch land in RAX. The nested div then borrows
	// RAX/RDX while condensing the comparison. The store address remains deferred
	// until after SETcc, so losing the outer RAX reservation lets address
	// generation clobber AL before the byte store.
	body := []byte{0x00}
	for off := byte(0); off < 4; off++ {
		body = append(body,
			0x20, 0x00, // local.get loadBase
			0x41, off, // i32.const off
			0x6a,             // i32.add
			0x2d, 0x00, 0x00, // i32.load8_u (deferred)
		)
	}
	body = append(body,
		0x20, 0x01, 0x41, 0x25, 0x6a, // address = storeBase + 37
		0x20, 0x02, 0x20, 0x03, 0x6e, // x / y (fixed RAX/RDX)
		0x20, 0x04, 0x49, // (x / y) <u z
		0x3a, 0x00, 0x00, // i32.store8
	)
	for range 4 {
		body = append(body, 0x1a) // drop the pressure loads
	}
	body = append(body,
		0x20, 0x01, 0x41, 0x25, 0x6a,
		0x2d, 0x00, 0x00, // return stored byte
		0x0b,
	)

	m := modMem(t, 1,
		[]wasm.ValType{wasm.I32, wasm.I32, wasm.I32, wasm.I32, wasm.I32},
		[]wasm.ValType{wasm.I32}, body)
	got, mem, err := runMemAmd64(t, m, nil, 0, 100, 100, 5, 30)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got != 1 || mem[137] != 1 {
		t.Fatalf("fixed-register pressure corrupted store8 flags: result=%d mem[137]=%d, want 1", got, mem[137])
	}
}
