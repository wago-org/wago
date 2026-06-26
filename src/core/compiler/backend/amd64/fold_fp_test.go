//go:build linux && amd64

package amd64

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func foldI32(t *testing.T, body string) int32 {
	t.Helper()
	return runI32(t, watToModule(t, `(module (func (export "f") (result i32) `+body+`))`))
}

// TestConstFoldIntUnaryConv covers clz/ctz/popcnt and wrap/extend folding.
func TestConstFoldIntUnaryConv(t *testing.T) {
	cases := []struct {
		name, wat string
		want      int32
	}{
		{"clz", `i32.const 1 i32.clz`, 31},
		{"clz_zero", `i32.const 0 i32.clz`, 32},
		{"ctz", `i32.const 8 i32.ctz`, 3},
		{"popcnt", `i32.const 0xFF i32.popcnt`, 8},
		{"i64_clz", `i64.const 1 i64.clz i32.wrap_i64`, 63},
		{"i64_ctz", `i64.const 16 i64.ctz i32.wrap_i64`, 4},
		{"i64_popcnt", `i64.const -1 i64.popcnt i32.wrap_i64`, 64},
		{"wrap", `i64.const 4294967303 i32.wrap_i64`, 7}, // 0x1_0000_0007 -> 7
		{"extend_s", `i32.const -1 i64.extend_i32_s i64.const -1 i64.eq`, 1},
		{"extend_u", `i32.const -1 i64.extend_i32_u i64.const 4294967295 i64.eq`, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := foldI32(t, tc.wat); got != tc.want {
				t.Fatalf("%s: got %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}

// TestConstFoldFloatMisc covers float comparisons, neg/abs, and reinterprets
// (all produce an i32 result directly).
func TestConstFoldFloatMisc(t *testing.T) {
	cases := []struct {
		name, wat string
		want      int32
	}{
		{"f32_lt_true", `f32.const 1.5 f32.const 2.5 f32.lt`, 1},
		{"f32_lt_false", `f32.const 2.5 f32.const 1.5 f32.lt`, 0},
		{"f32_eq", `f32.const 1.5 f32.const 1.5 f32.eq`, 1},
		{"f32_ne", `f32.const 1.5 f32.const 2.5 f32.ne`, 1},
		{"f64_ge", `f64.const 2.0 f64.const 2.0 f64.ge`, 1},
		{"f64_gt_false", `f64.const 1.0 f64.const 2.0 f64.gt`, 0},
		{"f32_abs", `f32.const -1.5 f32.abs f32.const 1.5 f32.eq`, 1},
		{"f32_neg", `f32.const 1.5 f32.neg f32.const -1.5 f32.eq`, 1},
		{"f64_neg", `f64.const 2.5 f64.neg f64.const -2.5 f64.eq`, 1},
		{"f64_abs", `f64.const -3.5 f64.abs f64.const 3.5 f64.eq`, 1},
		{"reinterp_i2f", `i32.const 1069547520 f32.reinterpret_i32 f32.const 1.5 f32.eq`, 1}, // 0x3FC00000
		{"reinterp_f2i", `f32.const 1.5 i32.reinterpret_f32`, 1069547520},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := foldI32(t, tc.wat); got != tc.want {
				t.Fatalf("%s: got %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}

// TestConstFoldFloatArith checks that folded float arithmetic is bit-identical
// to Go's (SSE) computation — i.e. matches what the runtime op would produce.
func TestConstFoldFloatArith(t *testing.T) {
	f32 := []struct {
		name, op string
		a, b     float32
	}{
		{"f32_add", "f32.add", 1.5, 0.25},
		{"f32_sub", "f32.sub", 1.0, 0.3},
		{"f32_mul", "f32.mul", 1.1, 2.2},
		{"f32_div", "f32.div", 1.0, 3.0},
	}
	for _, c := range f32 {
		t.Run(c.name, func(t *testing.T) {
			var want float32
			switch c.op {
			case "f32.add":
				want = c.a + c.b
			case "f32.sub":
				want = c.a - c.b
			case "f32.mul":
				want = c.a * c.b
			case "f32.div":
				want = c.a / c.b
			}
			body := fmt.Sprintf("f32.const %v f32.const %v %s i32.reinterpret_f32", c.a, c.b, c.op)
			if got := uint32(foldI32(t, body)); got != math.Float32bits(want) {
				t.Fatalf("%s: bits %#x, want %#x", c.name, got, math.Float32bits(want))
			}
		})
	}

	f64 := []struct {
		name, op string
		a, b     float64
	}{
		{"f64_add", "f64.add", 1.1, 2.2},
		{"f64_mul", "f64.mul", 1.1, 2.2},
		{"f64_div", "f64.div", 1.0, 3.0},
	}
	for _, c := range f64 {
		t.Run(c.name, func(t *testing.T) {
			var want float64
			switch c.op {
			case "f64.add":
				want = c.a + c.b
			case "f64.mul":
				want = c.a * c.b
			case "f64.div":
				want = c.a / c.b
			}
			bits := int64(math.Float64bits(want))
			body := fmt.Sprintf("f64.const %v f64.const %v %s i64.reinterpret_f64 i64.const %d i64.eq", c.a, c.b, c.op, bits)
			if got := foldI32(t, body); got != 1 {
				t.Fatalf("%s: folded bits != expected", c.name)
			}
		})
	}
}

// TestConstFoldFloatNaNGuard proves arithmetic folds (no SSE op emitted) but a
// NaN-producing fold is left to codegen (the SSE op stays, NaN bits intact).
func TestConstFoldFloatNaNGuard(t *testing.T) {
	dis := func(body string) string {
		code, err := CompileFunction(watToModule(t, `(module (func (export "f") (result i32) `+body+`))`), 0)
		if err != nil {
			t.Fatal(err)
		}
		return disasm(t, code)
	}
	has := func(d, m string) bool {
		for _, line := range strings.Split(d, "\n") {
			for _, tok := range strings.Fields(line) {
				if tok == m {
					return true
				}
			}
		}
		return false
	}
	if d := dis(`f32.const 1.0 f32.const 2.0 f32.add i32.reinterpret_f32`); has(d, "addss") {
		t.Fatalf("expected folded f32.add (no addss):\n%s", d)
	}
	if d := dis(`f32.const inf f32.const inf f32.sub i32.reinterpret_f32`); !has(d, "subss") {
		t.Fatalf("expected NaN-producing inf-inf NOT folded (subss kept):\n%s", d)
	}
}

// TestConstFoldFloatCmpNaN checks that folding a float comparison with a
// constant NaN operand still yields the wasm-correct result (ordered compares
// false, ne true) — folding NaN is sound because Go's comparison operators
// match wasm semantics, and the runtime fcmp now agrees.
func TestConstFoldFloatCmpNaN(t *testing.T) {
	cases := []struct {
		name, wat string
		want      int32
	}{
		{"eq_nan", `f32.const nan f32.const 1.0 f32.eq`, 0},
		{"ne_nan", `f32.const nan f32.const 1.0 f32.ne`, 1},
		{"lt_nan", `f32.const nan f32.const 1.0 f32.lt`, 0},
		{"gt_nan", `f32.const 1.0 f32.const nan f32.gt`, 0},
		{"le_nan", `f32.const nan f32.const nan f32.le`, 0},
		{"f64_ge_nan", `f64.const nan f64.const 1.0 f64.ge`, 0},
		{"f64_ne_nan", `f64.const 1.0 f64.const nan f64.ne`, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := foldI32(t, tc.wat); got != tc.want {
				t.Fatalf("%s: got %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}
