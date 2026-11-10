//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func buildEqzBranchArm64(cmp byte, nEqz int) []byte {
	body := []byte{0x00, 0x20, 0x00, 0x41, 0x05, cmp}
	for i := 0; i < nEqz; i++ {
		body = append(body, 0x45)
	}
	return append(body, 0x04, 0x7f, 0x41, 0x0a, 0x05, 0x41, 0x14, 0x0b, 0x0b)
}

func TestEqzFoldArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	cases := []struct {
		cmp  byte
		nEqz int
		arg  int32
		want uint32
	}{
		{0x48, 0, 1, 10}, {0x48, 0, 9, 20},
		{0x48, 1, 1, 20}, {0x48, 1, 9, 10},
		{0x48, 2, 1, 10}, {0x46, 1, 5, 20}, {0x46, 1, 4, 10},
	}
	for _, tc := range cases {
		m := mod1(t, i32, i32, buildEqzBranchArm64(tc.cmp, tc.nEqz))
		got := uint32(runArm64Internal2(t, m, uintptr(uint32(tc.arg)), 0))
		if got != tc.want {
			t.Errorf("cmp=%#x eqz=%d arg=%d: got %d, want %d", tc.cmp, tc.nEqz, tc.arg, got, tc.want)
		}
	}

	m := mod1(t, i32, i32, buildEqzBranchArm64(0x48, 1))
	s := compileWithStats(t, m, false).Funcs[0]
	if s.Peephole["eqz-fold"] != 1 || s.Peephole["cmp-branch-fuse"] != 1 || s.Peephole["compare-setcc"] != 0 {
		t.Fatalf("unexpected fold counters: %v", s.Peephole)
	}
}

func TestEqzFoldKillSwitchEquivalentArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	saved := stFlagsEnabled
	defer func() { stFlagsEnabled = saved }()
	for _, cmp := range []byte{0x46, 0x48, 0x4e} {
		for nEqz := 0; nEqz <= 3; nEqz++ {
			body := buildEqzBranchArm64(cmp, nEqz)
			for _, arg := range []int32{1, 4, 5, 9} {
				stFlagsEnabled = true
				on := uint32(runArm64Internal2(t, mod1(t, i32, i32, body), uintptr(uint32(arg)), 0))
				stFlagsEnabled = false
				off := uint32(runArm64Internal2(t, mod1(t, i32, i32, body), uintptr(uint32(arg)), 0))
				if on != off {
					t.Fatalf("cmp=%#x eqz=%d arg=%d: on=%d off=%d", cmp, nEqz, arg, on, off)
				}
			}
		}
	}
}
