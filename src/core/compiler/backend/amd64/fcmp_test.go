//go:build linux && amd64

package amd64

import (
	"fmt"
	"testing"
)

func cmpI32(t *testing.T, body string) int32 {
	t.Helper()
	return runI32(t, watToModule(t, `(module (func (export "f") (result i32) `+body+`))`))
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
				wat := fmt.Sprintf("%s.const %s %s.const %s %s.%s", ty, p.a, ty, p.b, ty, op.name)
				want := op.want
				t.Run(name, func(t *testing.T) {
					if got := cmpI32(t, wat); got != want {
						t.Fatalf("%s: got %d, want %d", name, got, want)
					}
				})
			}
		}
	}
}

// TestFcmpOrdered: ordered comparisons must stay correct after the NaN fix.
func TestFcmpOrdered(t *testing.T) {
	cases := []struct {
		name, wat string
		want      int32
	}{
		{"lt_true", `f32.const 1.0 f32.const 2.0 f32.lt`, 1},
		{"lt_false", `f32.const 2.0 f32.const 1.0 f32.lt`, 0},
		{"lt_eq", `f32.const 1.0 f32.const 1.0 f32.lt`, 0},
		{"le_lt", `f32.const 1.0 f32.const 2.0 f32.le`, 1},
		{"le_eq", `f32.const 1.0 f32.const 1.0 f32.le`, 1},
		{"le_false", `f32.const 2.0 f32.const 1.0 f32.le`, 0},
		{"gt_true", `f32.const 2.0 f32.const 1.0 f32.gt`, 1},
		{"gt_false", `f32.const 1.0 f32.const 2.0 f32.gt`, 0},
		{"ge_eq", `f32.const 1.0 f32.const 1.0 f32.ge`, 1},
		{"ge_false", `f32.const 1.0 f32.const 2.0 f32.ge`, 0},
		{"eq_true", `f32.const 1.0 f32.const 1.0 f32.eq`, 1},
		{"eq_false", `f32.const 1.0 f32.const 2.0 f32.eq`, 0},
		{"ne_true", `f32.const 1.0 f32.const 2.0 f32.ne`, 1},
		{"ne_false", `f32.const 1.0 f32.const 1.0 f32.ne`, 0},
		{"f64_lt_true", `f64.const 1.0 f64.const 2.0 f64.lt`, 1},
		{"f64_le_eq", `f64.const 1.0 f64.const 1.0 f64.le`, 1},
		{"f64_gt_true", `f64.const 2.0 f64.const 1.0 f64.gt`, 1},
		{"f64_ge_eq", `f64.const 1.0 f64.const 1.0 f64.ge`, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cmpI32(t, tc.wat); got != tc.want {
				t.Fatalf("%s: got %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}
