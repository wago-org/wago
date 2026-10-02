//go:build arm64

package arm64

import (
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// TestFloatCompareBranchFusion exercises the fused float-compare→branch path
// (fcmpMaybeDefer → condenseFCompareToFlags): an ordered float relation
// (lt/le/gt/ge) directly before `if` or `br_if` lowers to FCMP + a NaN-safe
// B.cond instead of materializing a boolean. Go's native <,>,<=,>= return false
// on NaN, matching wasm, so they are the oracle.
func TestFloatCompareBranchFusion(t *testing.T) {
	fbits32 := func(v float32) uint64 { return uint64(math.Float32bits(v)) }
	fbits64 := func(v float64) uint64 { return math.Float64bits(v) }

	type opc struct {
		name         string
		f32Op, f64Op byte
		predFn       func(a, b float64) bool
	}
	ops := []opc{
		{"lt", 0x5d, 0x63, func(a, b float64) bool { return a < b }},
		{"gt", 0x5e, 0x64, func(a, b float64) bool { return a > b }},
		{"le", 0x5f, 0x65, func(a, b float64) bool { return a <= b }},
		{"ge", 0x60, 0x66, func(a, b float64) bool { return a >= b }},
	}
	inputs := []struct{ a, b float64 }{
		{1, 2}, {2, 1}, {1, 1}, {-1, 1}, {1, -1},
		{math.NaN(), 1}, {1, math.NaN()}, {math.NaN(), math.NaN()},
		{math.Inf(1), 1}, {1, math.Inf(1)}, {math.Inf(-1), math.Inf(1)},
		{math.Copysign(0, -1), 0},
	}
	ifBody := func(op byte) []byte {
		return []byte{0x00,
			0x20, 0x00, 0x20, 0x01, op,
			0x04, 0x7f, 0x41, 0x01, 0x05, 0x41, 0x00, 0x0b, 0x0b}
	}
	brIfBody := func(op byte) []byte {
		return []byte{0x00,
			0x02, 0x7f, 0x41, 0x01, 0x20, 0x00, 0x20, 0x01, op,
			0x0d, 0x00, 0x1a, 0x41, 0x00, 0x0b, 0x0b}
	}
	run := func(t *testing.T, typ wasm.ValType, body []byte, a, b float64) uint32 {
		m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{wasm.I32}, body)
		var av, bv uint64
		if typ == wasm.F32 {
			av, bv = fbits32(float32(a)), fbits32(float32(b))
		} else {
			av, bv = fbits64(a), fbits64(b)
		}
		return uint32(runArm64u(t, m, av, bv))
	}

	for _, o := range ops {
		o := o
		for _, form := range []struct {
			tag  string
			body func(byte) []byte
		}{{"if", ifBody}, {"br_if", brIfBody}} {
			for _, ft := range []struct {
				name string
				typ  wasm.ValType
				code byte
			}{{"f32", wasm.F32, o.f32Op}, {"f64", wasm.F64, o.f64Op}} {
				body := form.body(ft.code)
				t.Run(ft.name+"."+o.name+"/"+form.tag, func(t *testing.T) {
					for _, in := range inputs {
						a, b := in.a, in.b
						if ft.typ == wasm.F32 {
							a, b = float64(float32(a)), float64(float32(b))
						}
						want := uint32(0)
						if o.predFn(a, b) {
							want = 1
						}
						if got := run(t, ft.typ, body, in.a, in.b); got != want {
							t.Fatalf("%s.%s/%s(%v,%v) = %d, want %d", ft.name, o.name, form.tag, in.a, in.b, got, want)
						}
					}
				})
			}
		}
	}
}
