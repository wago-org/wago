//go:build linux && amd64

package amd64

import "testing"

func cmpI32(t *testing.T, body string) int32 {
	t.Helper()
	return runI32(t, watToModule(t, `(module (func (export "f") (result i32) `+body+`))`))
}

// TestFcmpNaN: any comparison involving NaN is unordered — every ordered
// comparison (eq/lt/gt/le/ge) is false and ne is true.
func TestFcmpNaN(t *testing.T) {
	cases := []struct {
		name, wat string
		want      int32
	}{
		{"eq_nan", `f32.const nan f32.const 1.0 f32.eq`, 0},
		{"ne_nan", `f32.const nan f32.const 1.0 f32.ne`, 1},
		{"lt_nan", `f32.const nan f32.const 1.0 f32.lt`, 0},
		{"gt_nan", `f32.const nan f32.const 1.0 f32.gt`, 0},
		{"le_nan", `f32.const nan f32.const 1.0 f32.le`, 0},
		{"ge_nan", `f32.const nan f32.const 1.0 f32.ge`, 0},
		{"lt_nan_rhs", `f32.const 1.0 f32.const nan f32.lt`, 0},
		{"gt_nan_rhs", `f32.const 1.0 f32.const nan f32.gt`, 0},
		{"le_nan_rhs", `f32.const 1.0 f32.const nan f32.le`, 0},
		{"eq_nan_both", `f32.const nan f32.const nan f32.eq`, 0},
		{"ne_nan_both", `f32.const nan f32.const nan f32.ne`, 1},
		{"f64_eq_nan", `f64.const nan f64.const 1.0 f64.eq`, 0},
		{"f64_ne_nan", `f64.const nan f64.const 1.0 f64.ne`, 1},
		{"f64_lt_nan", `f64.const nan f64.const 1.0 f64.lt`, 0},
		{"f64_le_nan", `f64.const nan f64.const 1.0 f64.le`, 0},
		{"f64_ge_nan", `f64.const nan f64.const 1.0 f64.ge`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cmpI32(t, tc.wat); got != tc.want {
				t.Fatalf("%s: got %d, want %d", tc.name, got, tc.want)
			}
		})
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
