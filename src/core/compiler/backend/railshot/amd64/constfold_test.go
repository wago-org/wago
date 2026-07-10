//go:build amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// The const-fold pack (docs/no-ir-plan.md P2.3/P2.4): relational compares and the
// unary ops (clz/ctz/popcnt/eqz + width conversions) fold at compile time when
// their operands are constant, and integer compares of the same local collapse to
// a constant. These only fire on compile-time-known inputs, so the fold value is
// checked against Go's own arithmetic and against end-to-end execution.

func TestFoldCompareUnit(t *testing.T) {
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

func TestFoldUnaryConstUnit(t *testing.T) {
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

// TestConstFoldPackExec runs const-folded bodies end-to-end so the folded value is
// validated against the real runtime, not just the fold helper.
func TestConstFoldPackExec(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	i64 := []wasm.ValType{wasm.I64}
	// i32-result cases: (body without leading local-decl byte / trailing end).
	i32cases := []struct {
		name string
		body []byte
		want int32
	}{
		// i32.const -1; i32.const 1; i32.lt_s → 1
		{"lt_s", []byte{0x41, 0x7f, 0x41, 0x01, 0x48}, 1},
		// i32.const -1; i32.const 1; i32.lt_u → 0
		{"lt_u", []byte{0x41, 0x7f, 0x41, 0x01, 0x49}, 0},
		// i32.const 5; i32.const 5; i32.eq → 1
		{"eq", []byte{0x41, 0x05, 0x41, 0x05, 0x46}, 1},
		// i32.const 0; i32.eqz → 1
		{"eqz_zero", []byte{0x41, 0x00, 0x45}, 1},
		// i32.const 1; i32.clz → 31
		{"clz", []byte{0x41, 0x01, 0x67}, 31},
		// i32.const 8; i32.ctz → 3
		{"ctz", []byte{0x41, 0x08, 0x68}, 3},
		// i32.const 6; i32.popcnt → 2
		{"popcnt", []byte{0x41, 0x06, 0x69}, 2},
		// i64.const 7; i32.wrap_i64 → 7
		{"wrap", []byte{0x42, 0x07, 0xa7}, 7},
		// i32.const 255; i32.extend8_s → -1
		{"extend8_s", []byte{0x41, 0xff, 0x01, 0xc0}, -1},
	}
	for _, c := range i32cases {
		t.Run("i32/"+c.name, func(t *testing.T) {
			body := append(append([]byte{0x00}, c.body...), 0x0b)
			m := mod1(t, nil, i32, body)
			if got := runAmd64(t, m); got != c.want {
				t.Errorf("%s = %d, want %d", c.name, got, c.want)
			}
		})
	}

	// i64-result cases.
	i64cases := []struct {
		name string
		body []byte
		want uint64
	}{
		// i64.const 1; i64.clz → 63
		{"clz", []byte{0x42, 0x01, 0x79}, 63},
		// i32.const -1; i64.extend_i32_u → 0xFFFFFFFF
		{"extend_i32_u", []byte{0x41, 0x7f, 0xad}, 0xFFFFFFFF},
		// i32.const -1; i64.extend_i32_s → all ones
		{"extend_i32_s", []byte{0x41, 0x7f, 0xac}, 0xFFFFFFFFFFFFFFFF},
	}
	for _, c := range i64cases {
		t.Run("i64/"+c.name, func(t *testing.T) {
			body := append(append([]byte{0x00}, c.body...), 0x0b)
			m := mod1(t, nil, i64, body)
			if got := runAmd64u(t, m); got != c.want {
				t.Errorf("%s = %#x, want %#x", c.name, got, c.want)
			}
		})
	}
}

// TestSameOperandCompareExec checks the same-local integer compare identities
// (x==x→1, x<x→0) fold and execute correctly regardless of the argument value.
func TestSameOperandCompareExec(t *testing.T) {
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
				if got := runAmd64(t, m, arg); got != c.want {
					t.Errorf("%s(%d) = %d, want %d", c.name, arg, got, c.want)
				}
			}
		})
	}
}

// TestConstFoldPackPeep proves the fold actually happened: a constant compare
// bumps "const-fold" (not "compare-setcc", which would mean a runtime SETcc was
// emitted), a constant unary bumps "const-fold", and a same-local compare bumps
// "same-operand".
func TestConstFoldPackPeep(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	cases := []struct {
		name string
		in   []wasm.ValType
		out  []wasm.ValType
		body []byte
		peep string
	}{
		{
			// i32.const 5; i32.const 3; i32.lt_s → folded to const 0
			name: "compare-fold", out: i32,
			body: []byte{0x00, 0x41, 0x05, 0x41, 0x03, 0x48, 0x0b},
			peep: "const-fold",
		},
		{
			// i32.const 8; i32.ctz → folded to const 3
			name: "unary-fold", out: i32,
			body: []byte{0x00, 0x41, 0x08, 0x68, 0x0b},
			peep: "const-fold",
		},
		{
			// local.get 0; local.get 0; i32.eq → same-operand → const 1
			name: "same-operand-compare", in: i32, out: i32,
			body: []byte{0x00, 0x20, 0x00, 0x20, 0x00, 0x46, 0x0b},
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
			// A folded compare must NOT have emitted a runtime SETcc.
			if got := s.Peephole["compare-setcc"]; got != 0 {
				t.Errorf("compare-setcc = %d, want 0 (fold should have elided it; all: %v)", got, s.Peephole)
			}
			// Nothing should have been condensed: the whole body folds to a const.
			if s.Condenses != 0 {
				t.Errorf("Condenses = %d, want 0 (body folds to a constant)", s.Condenses)
			}
		})
	}
}
