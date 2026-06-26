//go:build linux && amd64

package amd64

import (
	"fmt"
	"testing"
)

// fcmpRT compiles `(a TY.OP b)` with both operands routed through locals so
// they stay non-constant — this exercises the runtime fcmp lowering rather than
// being intercepted by constant folding. ty is "f32"/"f64", a/b are wat float
// literals (e.g. "nan", "1.0").
func fcmpRT(t *testing.T, ty, op, a, b string) int32 {
	t.Helper()
	wat := fmt.Sprintf(`(module (func (export "f") (result i32) (local %[1]s %[1]s)
		%[1]s.const %[2]s local.set 0
		%[1]s.const %[3]s local.set 1
		local.get 0 local.get 1 %[1]s.%[4]s))`, ty, a, b, op)
	return runI32(t, watToModule(t, wat))
}

// TestFcmpNaN exhaustively checks that any comparison involving NaN is
// unordered — every ordered comparison (eq/lt/gt/le/ge) is false, ne is true —
// across all six ops, both widths, with NaN on the left, right, and both sides.
func TestFcmpNaN(t *testing.T) {
	ops := []struct {
		name string
		want int32 // result when an operand is NaN
	}{
		{"eq", 0}, {"ne", 1}, {"lt", 0}, {"gt", 0}, {"le", 0}, {"ge", 0},
	}
	placements := []struct{ name, a, b string }{
		{"lhs", "nan", "1.0"},
		{"rhs", "1.0", "nan"},
		{"both", "nan", "nan"},
	}
	for _, ty := range []string{"f32", "f64"} {
		for _, op := range ops {
			for _, p := range placements {
				name := fmt.Sprintf("%s_%s_%s", ty, op.name, p.name)
				ty, op, p := ty, op, p
				t.Run(name, func(t *testing.T) {
					if got := fcmpRT(t, ty, op.name, p.a, p.b); got != op.want {
						t.Fatalf("%s: got %d, want %d", name, got, op.want)
					}
				})
			}
		}
	}
}

// TestFcmpOrdered: ordered comparisons must stay correct after the NaN fix.
// Operands go through locals so this tests the runtime fcmp, not const folding.
func TestFcmpOrdered(t *testing.T) {
	cases := []struct {
		name, ty, op, a, b string
		want               int32
	}{
		{"lt_true", "f32", "lt", "1.0", "2.0", 1},
		{"lt_false", "f32", "lt", "2.0", "1.0", 0},
		{"lt_eq", "f32", "lt", "1.0", "1.0", 0},
		{"le_lt", "f32", "le", "1.0", "2.0", 1},
		{"le_eq", "f32", "le", "1.0", "1.0", 1},
		{"le_false", "f32", "le", "2.0", "1.0", 0},
		{"gt_true", "f32", "gt", "2.0", "1.0", 1},
		{"gt_false", "f32", "gt", "1.0", "2.0", 0},
		{"ge_eq", "f32", "ge", "1.0", "1.0", 1},
		{"ge_false", "f32", "ge", "1.0", "2.0", 0},
		{"eq_true", "f32", "eq", "1.0", "1.0", 1},
		{"eq_false", "f32", "eq", "1.0", "2.0", 0},
		{"ne_true", "f32", "ne", "1.0", "2.0", 1},
		{"ne_false", "f32", "ne", "1.0", "1.0", 0},
		{"f64_lt_true", "f64", "lt", "1.0", "2.0", 1},
		{"f64_le_eq", "f64", "le", "1.0", "1.0", 1},
		{"f64_gt_true", "f64", "gt", "2.0", "1.0", 1},
		{"f64_ge_eq", "f64", "ge", "1.0", "1.0", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fcmpRT(t, tc.ty, tc.op, tc.a, tc.b); got != tc.want {
				t.Fatalf("%s: got %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}
