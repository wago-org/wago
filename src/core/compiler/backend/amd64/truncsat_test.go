//go:build linux && amd64

package amd64

import (
	"fmt"
	"testing"
)

func tsI32(t *testing.T, body string) int32 {
	t.Helper()
	return runI32(t, watToModule(t, `(module (func (export "f") (result i32) `+body+`))`))
}

// TestTruncSatI32 covers the four i32 trunc_sat variants: in-range truncation,
// NaN -> 0, ±inf and huge magnitudes saturating to the bounds, and (unsigned)
// negatives saturating to 0.
func TestTruncSatI32(t *testing.T) {
	cases := []struct {
		name, wat string
		want      int32
	}{
		// i32.trunc_sat_f32_s
		{"s_f32_pos", `f32.const 3.7 i32.trunc_sat_f32_s`, 3},
		{"s_f32_neg", `f32.const -3.7 i32.trunc_sat_f32_s`, -3},
		{"s_f32_nan", `f32.const nan i32.trunc_sat_f32_s`, 0},
		{"s_f32_inf", `f32.const inf i32.trunc_sat_f32_s`, 2147483647},
		{"s_f32_ninf", `f32.const -inf i32.trunc_sat_f32_s`, -2147483648},
		{"s_f32_big", `f32.const 1e30 i32.trunc_sat_f32_s`, 2147483647},
		{"s_f32_nbig", `f32.const -1e30 i32.trunc_sat_f32_s`, -2147483648},
		// i32.trunc_sat_f32_u
		{"u_f32_pos", `f32.const 3.7 i32.trunc_sat_f32_u`, 3},
		{"u_f32_neg", `f32.const -1 i32.trunc_sat_f32_u`, 0},
		{"u_f32_nan", `f32.const nan i32.trunc_sat_f32_u`, 0},
		{"u_f32_inf", `f32.const inf i32.trunc_sat_f32_u`, -1},                // 0xFFFFFFFF
		{"u_f32_hi", `f32.const 2147483648 i32.trunc_sat_f32_u`, -2147483648}, // 2^31, in range for u32
		// i32.trunc_sat_f64_s / _u (f64 represents integers exactly)
		{"s_f64_neg", `f64.const -3.7 i32.trunc_sat_f64_s`, -3},
		{"s_f64_ninf", `f64.const -inf i32.trunc_sat_f64_s`, -2147483648},
		{"u_f64_max_inrange", `f64.const 4294967295 i32.trunc_sat_f64_u`, -1}, // 2^32-1, in range
		{"u_f64_over", `f64.const 4294967296 i32.trunc_sat_f64_u`, -1},        // 2^32 -> max
		{"u_f64_mid", `f64.const 4000000000 i32.trunc_sat_f64_u`, -294967296}, // int32(uint32(4e9))
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tsI32(t, tc.wat); got != tc.want {
				t.Fatalf("%s: got %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}

// TestTruncSatI64 covers the four i64 variants; the result is compared against
// an expected i64 constant so runI32 reads a 0/1.
func TestTruncSatI64(t *testing.T) {
	cases := []struct {
		name, expr string
		want       int64
	}{
		// i64.trunc_sat_f64_s
		{"s_pos", `f64.const 3.7 i64.trunc_sat_f64_s`, 3},
		{"s_neg", `f64.const -3.7 i64.trunc_sat_f64_s`, -3},
		{"s_nan", `f64.const nan i64.trunc_sat_f64_s`, 0},
		{"s_inf", `f64.const inf i64.trunc_sat_f64_s`, 9223372036854775807},
		{"s_ninf", `f64.const -inf i64.trunc_sat_f64_s`, -9223372036854775808},
		// i64.trunc_sat_f32_s
		{"s_f32_inf", `f32.const inf i64.trunc_sat_f32_s`, 9223372036854775807},
		// i64.trunc_sat_f64_u
		{"u_pos", `f64.const 3.7 i64.trunc_sat_f64_u`, 3},
		{"u_neg", `f64.const -1 i64.trunc_sat_f64_u`, 0},
		{"u_nan", `f64.const nan i64.trunc_sat_f64_u`, 0},
		{"u_inf", `f64.const inf i64.trunc_sat_f64_u`, -1},                                   // 0xFFFF...FFFF = uint64 max
		{"u_p63", `f64.const 9223372036854775808 i64.trunc_sat_f64_u`, -9223372036854775808}, // 2^63 (bias path)
		{"u_over", `f64.const 1e20 i64.trunc_sat_f64_u`, -1},                                 // >= 2^64 -> max
		// i64.trunc_sat_f32_u, bias path
		{"u_f32_p63", `f32.const 9223372036854775808 i64.trunc_sat_f32_u`, -9223372036854775808},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wat := fmt.Sprintf(`(module (func (export "f") (result i32) %s i64.const %d i64.eq))`, tc.expr, tc.want)
			if got := runI32(t, watToModule(t, wat)); got != 1 {
				t.Fatalf("%s: result != %d", tc.name, tc.want)
			}
		})
	}
}
