//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestVariableMaskCondition(t *testing.T) {
	saved := swarMaskTestEnabled
	defer func() { swarMaskTestEnabled = saved }()
	for _, typ := range []wasm.ValType{wasm.I32, wasm.I64} {
		and, eqz := byte(0x71), byte(0x45)
		if typ == wasm.I64 {
			and, eqz = 0x83, 0x50
		}
		for _, negate := range []bool{false, true} {
			if typ == wasm.I64 && !negate {
				continue
			}
			body := []byte{0, 0x3f, 0, 0x1a, 0x20, 0, 0x20, 1, and}
			if negate {
				body = append(body, eqz)
			}
			body = append(body, 0x04, 0x7f, 0x41, 11, 0x05, 0x41, 23, 0x0b, 0x0b)
			m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{wasm.I32}, body)
			m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
			for _, enabled := range []bool{false, true} {
				swarMaskTestEnabled = enabled
				if diagnosticsEnabled {
					stats := &ModuleStats{}
					cm, err := CompileModuleWith(m, CompileOptions{Stats: stats})
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						cm.CodeImage.Close()
					}
					if (stats.Funcs[0].Peephole["mask-condition-test"] != 0) != enabled {
						t.Fatalf("type=%v negate=%v enabled=%v stats=%v", typ, negate, enabled, stats.Funcs[0].Peephole)
					}
				}
				for _, in := range [][2]uint64{{0, 0}, {0x80, 0x80}, {0x80, 0x40}, {0xffffffff, 0x80000000}, {1 << 40, 1 << 40}, {^uint64(0), 0x123456789abcdef0}} {
					mask := in[0] & in[1]
					if typ == wasm.I32 {
						mask = uint64(uint32(mask))
					}
					condition := mask != 0
					if negate {
						condition = !condition
					}
					want := uint64(23)
					if condition {
						want = 11
					}
					if got := runArm64u(t, m, in[:]...); got != want {
						t.Fatalf("type=%v negate=%v enabled=%v inputs=%x got=%d want=%d", typ, negate, enabled, in, got, want)
					}
				}
			}
		}
	}
}

func TestMaskTeeBranchPreservesStoredBits(t *testing.T) {
	// The branch consumes truth, but local.tee must keep the entire bitmask.
	body := []byte{1, 1, 0x7f, 0x3f, 0, 0x1a, 0x02, 0x40, 0x20, 0, 0x20, 1, 0x71, 0x22, 2, 0x0d, 0, 0x41, 7, 0x21, 2, 0x0b, 0x20, 2, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
	for _, in := range [][2]uint64{{0, 0}, {0x80, 0xff}, {0x80000000, 0xffffffff}, {0x12345678, 0xf0f0f0f0}} {
		want := uint64(uint32(in[0] & in[1]))
		if want == 0 {
			want = 7
		}
		if got := runArm64u(t, m, in[:]...); got != want {
			t.Fatalf("inputs=%x got=%x want=%x", in, got, want)
		}
	}
}

func TestInvertedMaskCondition(t *testing.T) {
	saved := swarMaskTestEnabled
	defer func() { swarMaskTestEnabled = saved }()
	for _, typ := range []wasm.ValType{wasm.I32, wasm.I64} {
		constant, xor, and, eqz := byte(0x41), byte(0x73), byte(0x71), byte(0x45)
		if typ == wasm.I64 {
			constant, xor, and, eqz = 0x42, 0x85, 0x83, 0x50
		}
		body := []byte{0, 0x3f, 0, 0x1a, 0x20, 0, 0x20, 1, constant, 0x7f, xor, and, eqz, 0x0b}
		m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{wasm.I32}, body)
		m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
		for _, enabled := range []bool{false, true} {
			swarMaskTestEnabled = enabled
			for _, in := range [][2]uint64{{0, 0}, {0x80, 0x80}, {0x80, 0x40}, {1 << 40, 1 << 40}, {1 << 40, 0}, {^uint64(0), 0x123456789abcdef0}} {
				mask := in[0] &^ in[1]
				if typ == wasm.I32 {
					mask = uint64(uint32(mask))
				}
				want := uint64(0)
				if mask == 0 {
					want = 1
				}
				if got := runArm64u(t, m, in[:]...); got != want {
					t.Fatalf("type=%v enabled=%v inputs=%x got=%d want=%d", typ, enabled, in, got, want)
				}
			}
		}
	}
}

func TestZeroMaskConditionRetainsLoadTrap(t *testing.T) {
	for _, negate := range []bool{false, true} {
		body := []byte{0, 0x20, 0, 0x28, 2, 0, 0x41, 0, 0x71}
		if negate {
			body = append(body, 0x45)
		}
		body = append(body, 0x04, 0x7f, 0x41, 11, 0x05, 0x41, 23, 0x0b, 0x0b)
		m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
		m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
		for _, address := range []uint64{0, 65532, 65533, 0xffffffff} {
			got, err := runArm64Wrapper(t, m, address)
			if address > 65532 {
				if err == nil {
					t.Fatalf("negate=%v address=%x: masked load lost its trap", negate, address)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				want := uint64(23)
				if negate {
					want = 11
				}
				if got != want {
					t.Fatalf("negate=%v address=%x got=%d want=%d", negate, address, got, want)
				}
			}
		}
	}
}
