//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRepeatedLocalEarly(t *testing.T) {
	// Keep a constant and a potentially trapping load below the arithmetic root.
	// The local operand is read both within the left tree and on the right.
	body := []byte{0x00,
		0x41, 0x00, 0x41, 0x07, 0x36, 0x02, 0x00,
		0x41, 0x14, 0x20, 0x02, 0x28, 0x02, 0x00,
		0x20, 0x00, 0x20, 0x01, 0x73, 0x20, 0x01, 0x6b,
		0x6a, 0x6a, 0x0b}
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	saved := repeatedLocalEarly
	defer func() { repeatedLocalEarly = saved }()
	for _, enabled := range []bool{false, true} {
		repeatedLocalEarly = enabled
		for _, tc := range []struct{ a, b uint32 }{{0, 0}, {3, 7}, {0xffffffff, 0x80000000}, {0x80000000, 0xffffffff}} {
			got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{}, func(mem []byte) {
				binary.LittleEndian.PutUint32(mem[4:], 123)
			}, uint64(tc.a), uint64(tc.b), 4)
			want := uint32(20 + 123 + (tc.a ^ tc.b) - tc.b)
			if err != nil || uint32(got) != want || mem[0] != 7 {
				t.Fatalf("enabled=%v a=%x b=%x got=%x want=%x memory=%d err=%v", enabled, tc.a, tc.b, got, want, mem[0], err)
			}
		}
		_, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{}, nil, 3, 7, 65536)
		if err == nil || mem[0] != 7 {
			t.Fatalf("enabled=%v: expected trap after store: memory=%d err=%v", enabled, mem[0], err)
		}
	}
}

func TestRepeatedLocalEarlySelection(t *testing.T) {
	requireCompilerDiagnostics(t)
	body := []byte{0x01, 0x01, 0x7f, 0x03, 0x40,
		0x20, 0x01, 0x41, 0x0a, 0x73,
		0x20, 0x00, 0x20, 0x01, 0x6b, 0x22, 0x00,
		0x20, 0x00, 0x41, 0x1f, 0x75, 0x22, 0x00,
		0x73, 0x20, 0x00, 0x6b,
		0x6a, 0x21, 0x02,
		0x20, 0x01, 0x41, 0x01, 0x6b, 0x22, 0x01, 0x0d, 0x00,
		0x0b, 0x20, 0x02, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	saved := repeatedLocalEarly
	defer func() { repeatedLocalEarly = saved }()
	for _, enabled := range []bool{false, true} {
		repeatedLocalEarly = enabled
		s := compileWithStats(t, m, false).Funcs[0]
		if got := s.Peephole["repeated-local-early"]; (got != 0) != enabled {
			t.Fatalf("enabled=%v selection=%d stats=%v", enabled, got, s.Peephole)
		}
		for _, a := range []int32{0, 1, 2, -1, -2147483648, 2147483647} {
			x := a - 1
			sign := x >> 31
			want := int32(uint32(x^sign) - uint32(sign) + 11)
			if got := runAmd64(t, m, a, 1); got != want {
				t.Fatalf("enabled=%v a=%d got=%d want=%d", enabled, a, got, want)
			}
		}
	}
}

func TestRepeatedLocalEarlyTrapOrder(t *testing.T) {
	body := []byte{0x01, 0x01, 0x7f, 0x03, 0x40,
		0x41, 0x00, 0x41, 0x07, 0x36, 0x02, 0x00,
		0x41, 0x14, 0x20, 0x02, 0x28, 0x02, 0x00, 0x41, 0x01, 0x6a,
		0x20, 0x00, 0x20, 0x01, 0x6b, 0x22, 0x00,
		0x20, 0x00, 0x41, 0x1f, 0x75, 0x22, 0x00,
		0x73, 0x20, 0x00, 0x6b, 0x6a, 0x6a, 0x21, 0x03,
		0x20, 0x01, 0x41, 0x01, 0x6b, 0x22, 0x01, 0x0d, 0x00,
		0x0b, 0x20, 0x03, 0x0b}
	m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	saved := repeatedLocalEarly
	defer func() { repeatedLocalEarly = saved }()
	for _, enabled := range []bool{false, true} {
		repeatedLocalEarly = enabled
		var stats ModuleStats
		got, mem, err := runMemAmd64WithOptions(t, m, CompileOptions{Stats: optionalTestStats(&stats)}, func(mem []byte) {
			binary.LittleEndian.PutUint32(mem[4:], 123)
		}, 3, 1, 4)
		if err != nil || got != 146 || mem[0] != 7 {
			t.Fatalf("enabled=%v got=%d memory=%d err=%v", enabled, got, mem[0], err)
		}
		if diagnosticsEnabled && (stats.Funcs[0].Peephole["repeated-local-early"] != 0) != enabled {
			t.Fatalf("enabled=%v stats=%v", enabled, stats.Funcs[0].Peephole)
		}
		_, mem, err = runMemAmd64WithOptions(t, m, CompileOptions{}, nil, 3, 1, 65536)
		if err == nil || mem[0] != 7 {
			t.Fatalf("enabled=%v expected trap after store: memory=%d err=%v", enabled, mem[0], err)
		}
	}
}
