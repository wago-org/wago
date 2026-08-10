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
