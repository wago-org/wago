//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func swarMaskEqzBody() []byte {
	b := []byte{0x00, 0x20, 0x00, 0x42}
	b = append(b, wasmtest.SLEB64(int64(-9187201950435737472))...)
	return append(b, 0x83, 0x50, 0x0b)
}

func swarMaskBranchBody() []byte {
	b := swarMaskEqzBody()
	b = b[:len(b)-1]
	return append(b, 0x04, 0x7f, 0x41, 0x01, 0x05, 0x41, 0x00, 0x0b, 0x0b)
}

func TestSWARMaskTestFusion(t *testing.T) {
	i64, i32 := []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}
	m := mod1(t, i64, i32, swarMaskEqzBody())
	s := compileWithStats(t, m, false).Funcs[0]
	if got := s.Peephole["swar-mask-test"]; got != 1 {
		t.Fatalf("swar-mask-test = %d, want 1 (all: %v)", got, s.Peephole)
	}
	var off *CodegenStats
	func() {
		saved := swarMaskTestEnabled
		defer func() { swarMaskTestEnabled = saved }()
		swarMaskTestEnabled = false
		off = compileWithStats(t, m, false).Funcs[0]
	}()
	if s.CodeBytes >= off.CodeBytes {
		t.Fatalf("fused code = %d bytes, unfused = %d; want smaller", s.CodeBytes, off.CodeBytes)
	}
	for x, want := range map[uint64]uint32{0: 1, 0x7f7f7f7f7f7f7f7f: 1, 0x80: 0, 0x8000000000000000: 0} {
		if got := uint32(runAmd64u(t, m, x)); got != want {
			t.Fatalf("x=%#x: got %d, want %d", x, got, want)
		}
	}
}

func TestSWARMaskTestKillSwitchEquivalent(t *testing.T) {
	saved := swarMaskTestEnabled
	defer func() { swarMaskTestEnabled = saved }()
	i64, i32 := []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}
	for _, x := range []uint64{0, 0x80, 0x8080, 0x7f7f7f7f7f7f7f7f} {
		swarMaskTestEnabled = true
		on := uint32(runAmd64u(t, mod1(t, i64, i32, swarMaskEqzBody()), x))
		swarMaskTestEnabled = false
		off := uint32(runAmd64u(t, mod1(t, i64, i32, swarMaskEqzBody()), x))
		if on != off {
			t.Fatalf("x=%#x: on=%d off=%d", x, on, off)
		}
	}
}

func TestSWARMaskBranchFusion(t *testing.T) {
	i64, i32 := []wasm.ValType{wasm.I64}, []wasm.ValType{wasm.I32}
	m := mod1(t, i64, i32, swarMaskBranchBody())
	s := compileWithStats(t, m, false).Funcs[0]
	if s.Peephole["swar-mask-test"] != 1 || s.Peephole["cmp-branch-fuse"] != 1 || s.Peephole["compare-setcc"] != 0 {
		t.Fatalf("unexpected branch-fusion counters: %v", s.Peephole)
	}
	for x, want := range map[uint64]uint32{0: 1, 0x7f7f: 1, 0x80: 0} {
		if got := uint32(runAmd64u(t, m, x)); got != want {
			t.Fatalf("x=%#x: got %d, want %d", x, got, want)
		}
	}
}
