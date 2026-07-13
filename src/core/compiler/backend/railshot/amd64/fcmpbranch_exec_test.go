//go:build linux && amd64

package amd64

import (
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// TestFloatCompareBranchFusion exercises the fused float-compare→branch path
// (fcmpMaybeDefer → condenseFCompareToFlags): an ordered float relation
// (lt/le/gt/ge) placed directly before `if` or `br_if` lowers to UCOMIS + a
// NaN-safe Jcc instead of materializing a boolean. Go's native <,>,<=,>= already
// return false on NaN, matching wasm, so they are the oracle.
func TestFloatCompareBranchFusion(t *testing.T) {
	const (
		nan  = "nan"
		pinf = "+inf"
		ninf = "-inf"
	)
	// opcodes: [lt, gt, le, ge] for f32 (0x5d..0x60) and f64 (0x63..0x66).
	type opc struct {
		name   string
		f32Op  byte
		f64Op  byte
		predFn func(a, b float64) bool
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
		{math.Copysign(0, -1), 0}, // -0.0 vs +0.0 (equal in IEEE)
	}

	// ifBody / brIfBody wrap opcode `op` (of the given float type) as
	//   (if (result i32) (<cmp> p0 p1) (then 1) (else 0))
	// and the br_if analogue, so the compare is the immediate branch predicate.
	ifBody := func(op byte) []byte {
		return []byte{0x00,
			0x20, 0x00, 0x20, 0x01, op, // local.get 0, local.get 1, <cmp>
			0x04, 0x7f, // if (result i32)
			0x41, 0x01, // i32.const 1
			0x05,       // else
			0x41, 0x00, // i32.const 0
			0x0b, // end if
			0x0b} // end func
	}
	brIfBody := func(op byte) []byte {
		return []byte{0x00,
			0x02, 0x7f, // block (result i32)
			0x41, 0x01, // i32.const 1 (value carried on branch)
			0x20, 0x00, 0x20, 0x01, op, // local.get 0, local.get 1, <cmp>
			0x0d, 0x00, // br_if 0
			0x1a,       // drop
			0x41, 0x00, // i32.const 0 (fall-through value)
			0x0b, // end block
			0x0b} // end func
	}

	run := func(t *testing.T, typ wasm.ValType, op byte, body []byte, a, b float64) uint32 {
		m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{i32}, body)
		var av, bv uint64
		if typ == wasm.F32 {
			av, bv = f32b(float32(a)), f32b(float32(b))
		} else {
			av, bv = f64b(a), f64b(b)
		}
		return uint32(runAmd64u(t, m, av, bv))
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
				t.Run(ft.name+"."+o.name+"/"+form.tag, func(t *testing.T) {
					body := form.body(ft.code)
					for _, in := range inputs {
						a, b := in.a, in.b
						if ft.typ == wasm.F32 {
							a, b = float64(float32(a)), float64(float32(b))
						}
						want := uint32(0)
						if o.predFn(a, b) {
							want = 1
						}
						got := run(t, ft.typ, ft.code, body, in.a, in.b)
						if got != want {
							t.Fatalf("%s.%s/%s(%v,%v) = %d, want %d", ft.name, o.name, form.tag, in.a, in.b, got, want)
						}
					}
				})
			}
		}
	}
}
