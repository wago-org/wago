//go:build (linux || darwin) && arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Constant-folding pack, ported from amd64/constfold_test.go. Relational compares
// and the unary ops (clz/ctz/popcnt/eqz + width conversions) fold at compile time
// when their operands are constant, and integer compares of the same local
// collapse to a constant. The fold helpers are architecture-neutral (shared
// fold.go), so the unit expectations match amd64 exactly; the exec and peephole
// cases prove the arm64 backend actually folds and runs the folded value.

func TestFoldCompareUnitArm64(t *testing.T) {
	cases := []struct {
		op   wOp
		a, b int64
		w    bool
		want int64
	}{
		// i32: signed vs unsigned disagree on the sign bit.
		{opLtS, -1, 1, false, 1}, // -1 <ₛ 1
		{opLtU, -1, 1, false, 0}, // 0xffffffff <ᵤ 1 is false
		{opGtU, -1, 1, false, 1},
		{opEq, 5, 5, false, 1},
		{opNe, 5, 5, false, 0},
		{opLeS, 5, 5, false, 1},
		{opGeU, 3, 7, false, 0},
		// i64: full-width signed/unsigned.
		{opLtS, -1, 1, true, 1},
		{opLtU, -1, 1, true, 0},
		{opEq, 1 << 40, 1 << 40, true, 1},
		{opGtS, 1 << 40, 1<<40 - 1, true, 1},
	}
	for _, c := range cases {
		if got := foldCompare(c.op, c.a, c.b, c.w); got != c.want {
			t.Errorf("foldCompare(%d, %d, %d, w=%v) = %d, want %d", c.op, c.a, c.b, c.w, got, c.want)
		}
	}
}

func TestFoldUnaryConstUnitArm64(t *testing.T) {
	cases := []struct {
		op    wOp
		a     int64
		typ   machineType
		want  int64
		wantT machineType
	}{
		{opEqz, 0, mtI32, 1, mtI32},
		{opEqz, 7, mtI32, 0, mtI32},
		{opEqz, 0, mtI64, 1, mtI32},
		{opClz, 1, mtI32, 31, mtI32},
		{opClz, 0, mtI32, 32, mtI32},
		{opClz, 1, mtI64, 63, mtI64},
		{opCtz, 8, mtI32, 3, mtI32},
		{opCtz, 0, mtI64, 64, mtI64},
		{opPopcnt, 6, mtI32, 2, mtI32},   // 0b110
		{opPopcnt, -1, mtI64, 64, mtI64}, // all bits set
		{opWrap, int64(0x1_0000_0007), mtI32, 7, mtI32},
		{opSExt32, int64(int32(-1)), mtI64, -1, mtI64},         // stays -1 (sign-extended)
		{opZExt32, int64(int32(-1)), mtI64, 0xFFFFFFFF, mtI64}, // zero-extended
		{opSExt8, 0xFF, mtI32, -1, mtI32},                      // low byte 0xFF → -1
		{opSExt8, 0xFF, mtI64, -1, mtI64},
		{opSExt16, 0x8000, mtI32, int64(int32(-32768)), mtI32},
	}
	for _, c := range cases {
		got, gotT, ok := foldUnaryConst(c.op, c.a, c.typ)
		if !ok {
			t.Errorf("foldUnaryConst(%d, %d, %v) not ok", c.op, c.a, c.typ)
			continue
		}
		if got != c.want || gotT != c.wantT {
			t.Errorf("foldUnaryConst(%d, %d, %v) = (%d, %v), want (%d, %v)",
				c.op, c.a, c.typ, got, gotT, c.want, c.wantT)
		}
	}
	// Non-unary ops are not foldable here.
	if _, _, ok := foldUnaryConst(opAdd, 1, mtI32); ok {
		t.Errorf("foldUnaryConst(opAdd) unexpectedly ok")
	}
}

// TestConstFoldPackExecArm64 runs const-folded bodies end-to-end so the folded
// value is validated against the real arm64 runtime, not just the fold helper.
func TestConstFoldPackExecArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	i64 := []wasm.ValType{wasm.I64}
	i32cases := []struct {
		name string
		body []byte
		want int32
	}{
		{"lt_s", []byte{0x41, 0x7f, 0x41, 0x01, 0x48}, 1}, // -1; 1; i32.lt_s → 1
		{"lt_u", []byte{0x41, 0x7f, 0x41, 0x01, 0x49}, 0}, // -1; 1; i32.lt_u → 0
		{"eq", []byte{0x41, 0x05, 0x41, 0x05, 0x46}, 1},   // 5; 5; i32.eq → 1
		{"eqz_zero", []byte{0x41, 0x00, 0x45}, 1},         // 0; i32.eqz → 1
		{"clz", []byte{0x41, 0x01, 0x67}, 31},             // 1; i32.clz → 31
		{"ctz", []byte{0x41, 0x08, 0x68}, 3},              // 8; i32.ctz → 3
		{"popcnt", []byte{0x41, 0x06, 0x69}, 2},           // 6; i32.popcnt → 2
		{"wrap", []byte{0x42, 0x07, 0xa7}, 7},             // i64 7; i32.wrap_i64 → 7
		{"extend8_s", []byte{0x41, 0xff, 0x01, 0xc0}, -1}, // 255; i32.extend8_s → -1
	}
	for _, c := range i32cases {
		t.Run("i32/"+c.name, func(t *testing.T) {
			body := append(append([]byte{0x00}, c.body...), 0x0b)
			m := mod1(t, nil, i32, body)
			if got := runArm64(t, m); got != c.want {
				t.Errorf("%s = %d, want %d", c.name, got, c.want)
			}
		})
	}

	i64cases := []struct {
		name string
		body []byte
		want uint64
	}{
		{"clz", []byte{0x42, 0x01, 0x79}, 63},                          // i64 1; i64.clz → 63
		{"extend_i32_u", []byte{0x41, 0x7f, 0xad}, 0xFFFFFFFF},         // -1; i64.extend_i32_u
		{"extend_i32_s", []byte{0x41, 0x7f, 0xac}, 0xFFFFFFFFFFFFFFFF}, // -1; i64.extend_i32_s
	}
	for _, c := range i64cases {
		t.Run("i64/"+c.name, func(t *testing.T) {
			body := append(append([]byte{0x00}, c.body...), 0x0b)
			m := mod1(t, nil, i64, body)
			if got := runArm64u(t, m); got != c.want {
				t.Errorf("%s = %#x, want %#x", c.name, got, c.want)
			}
		})
	}
}

// TestSameOperandCompareExecArm64 checks the same-local integer compare identities
// (x==x→1, x<x→0) fold and execute correctly regardless of the argument value.
func TestSameOperandCompareExecArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	cases := []struct {
		name string
		cmp  byte
		want int32
	}{
		{"eq", 0x46, 1},
		{"le_s", 0x4c, 1},
		{"ge_u", 0x4f, 1},
		{"ne", 0x47, 0},
		{"lt_s", 0x48, 0},
		{"gt_u", 0x4b, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// local.get 0; local.get 0; <cmp>
			body := []byte{0x00, 0x20, 0x00, 0x20, 0x00, c.cmp, 0x0b}
			m := mod1(t, i32, i32, body)
			for _, arg := range []int32{0, -1, 7, 1 << 30} {
				if got := runArm64(t, m, arg); got != c.want {
					t.Errorf("%s(%d) = %d, want %d", c.name, arg, got, c.want)
				}
			}
		})
	}
}

// TestConstFoldPackPeepArm64 proves the fold actually happened: a constant compare
// bumps "const-fold" (not "compare-setcc", which would mean a runtime CSET was
// emitted), a constant unary bumps "const-fold", and a same-local compare bumps
// "same-operand". The whole body folds to a constant, so nothing condenses.
func TestConstFoldPackPeepArm64(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	cases := []struct {
		name string
		in   []wasm.ValType
		out  []wasm.ValType
		body []byte
		peep string
	}{
		{
			name: "compare-fold", out: i32,
			body: []byte{0x00, 0x41, 0x05, 0x41, 0x03, 0x48, 0x0b}, // 5; 3; i32.lt_s → const 0
			peep: "const-fold",
		},
		{
			name: "unary-fold", out: i32,
			body: []byte{0x00, 0x41, 0x08, 0x68, 0x0b}, // 8; i32.ctz → const 3
			peep: "const-fold",
		},
		{
			name: "same-operand-compare", in: i32, out: i32,
			body: []byte{0x00, 0x20, 0x00, 0x20, 0x00, 0x46, 0x0b}, // local 0; local 0; i32.eq → const 1
			peep: "same-operand",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, tc.in, tc.out, tc.body)
			s := compileWithStats(t, m, false).Funcs[0]
			if got := s.Peephole[tc.peep]; got != 1 {
				t.Errorf("Peephole[%q] = %d, want 1 (all: %v)", tc.peep, got, s.Peephole)
			}
			// A folded compare must NOT have emitted a runtime CSET.
			if got := s.Peephole["compare-setcc"]; got != 0 {
				t.Errorf("compare-setcc = %d, want 0 (fold should have elided it; all: %v)", got, s.Peephole)
			}
			if s.Condenses != 0 {
				t.Errorf("Condenses = %d, want 0 (body folds to a constant)", s.Condenses)
			}
		})
	}
}
