//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// sum5Module is a call-free reg-ABI leaf with five integer parameters
// (p0+p1+p2+p3+p4). Its pin pool is [R12,R13,R14,R15,R10,R11,RBP], so the fifth
// hot param lands in R10 — a free incoming-argument register that entry-arg
// pinning made available (a 5-param leaf otherwise loses that pin).
func sum5Module(t *testing.T) *wasm.Module {
	return mod1(t, []wasm.ValType{i32, i32, i32, i32, i32}, []wasm.ValType{i32}, []byte{
		0x00,       // no declared locals
		0x20, 0x00, // p0
		0x20, 0x01, 0x6a, // + p1
		0x20, 0x02, 0x6a, // + p2
		0x20, 0x03, 0x6a, // + p3
		0x20, 0x04, 0x6a, // + p4
		0x0b, // end
	})
}

func TestEntryArgPinExec(t *testing.T) {
	m := sum5Module(t)
	for _, a := range [][5]int32{{1, 2, 3, 4, 5}, {10, 20, 30, 40, 50}, {-1, -2, 3, -4, 5}} {
		want := a[0] + a[1] + a[2] + a[3] + a[4]
		if got := runAmd64(t, m, a[0], a[1], a[2], a[3], a[4]); got != want {
			t.Errorf("sum5(%v) = %d, want %d", a, got, want)
		}
	}
}

func TestEntryArgPinFires(t *testing.T) {
	s := compileWithStats(t, sum5Module(t), false).Funcs[0]
	if s.Peephole["entry-arg-local-pin"] == 0 {
		t.Fatalf("entry-arg-local-pin = 0, want >=1 (all: %v)", s.Peephole)
	}
}

// TestEntryArgPinKillSwitchEquivalent verifies the pinning is behavior-neutral.
func TestEntryArgPinKillSwitchEquivalent(t *testing.T) {
	defer func(prev bool) { entryArgPinsEnabled = prev }(entryArgPinsEnabled)
	for _, a := range [][5]int32{{1, 2, 3, 4, 5}, {7, 0, -3, 100, 42}} {
		entryArgPinsEnabled = true
		on := runAmd64(t, sum5Module(t), a[0], a[1], a[2], a[3], a[4])
		entryArgPinsEnabled = false
		off := runAmd64(t, sum5Module(t), a[0], a[1], a[2], a[3], a[4])
		if on != off {
			t.Fatalf("sum5(%v): on=%d off=%d", a, on, off)
		}
	}
}
