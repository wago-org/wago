//go:build linux && amd64

package amd64

import (
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// TestFloatCommuteMemFold exercises the commutative float memory-fold swap
// (fbin: foldFloatMem(a) && !foldFloatMem(b) → swap so the load folds as the SSE
// r/m source). The body stores param a to memory then computes
//
//	(f64.load 0) <op> (local.get 1)
//
// which places the deferred load on the LEFT of the operator. For add/mul the
// operands are swapped and the load becomes `addsd/mulsd xmm_b, [mem]`; for
// sub/div (non-commutative) the swap must NOT fire and the load is materialized.
// IEEE add/mul are exactly commutative, so Go's a<op>b is the oracle across the
// full NaN/±inf/±0 grid.
func TestFloatCommuteMemFold(t *testing.T) {
	type opc struct {
		name   string
		f32Op  byte
		f64Op  byte
		fn     func(a, b float64) float64
		commut bool // true for add/mul (swap expected)
	}
	ops := []opc{
		{"add", 0x92, 0xa0, func(a, b float64) float64 { return a + b }, true},
		{"mul", 0x94, 0xa2, func(a, b float64) float64 { return a * b }, true},
		{"sub", 0x93, 0xa1, func(a, b float64) float64 { return a - b }, false},
		{"div", 0x95, 0xa3, func(a, b float64) float64 { return a / b }, false},
	}
	inputs := []struct{ a, b float64 }{
		{2, 3}, {3, 2}, {-1.5, 4.25}, {0, 5}, {5, 0},
		{math.NaN(), 1}, {1, math.NaN()},
		{math.Inf(1), 2}, {2, math.Inf(-1)}, {math.Inf(1), math.Inf(-1)},
		{math.Copysign(0, -1), 0}, {0, math.Copysign(0, -1)},
	}

	// body: store a@0, then (load 0) <op> b. align = log2(width).
	build := func(storeOp, loadOp, arithOp, align byte) []byte {
		return []byte{0x00, // no locals
			0x41, 0x00, // i32.const 0  (store addr)
			0x20, 0x00, // local.get 0  (a)
			storeOp, align, 0x00, // <f>.store  align off=0
			0x41, 0x00, // i32.const 0  (load addr)
			loadOp, align, 0x00, // <f>.load   -> memRef (LEFT operand)
			0x20, 0x01, // local.get 1  (b)
			arithOp, // <f>.<op>
			0x0b}    // end
	}

	memType := append([]byte{0x00}, wasmtest.ULEB(1)...) // min 1 page, no max
	modFor := func(t *testing.T, typ wasm.ValType, body []byte) *wasm.Module {
		t.Helper()
		entry := append(wasmtest.ULEB(uint32(len(body))), body...)
		b := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ}, []wasm.ValType{typ}))),
			wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
			wasmtest.Section(5, wasmtest.Vec(memType)),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(entry)),
		)
		m, err := wasm.DecodeModule(b)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		return m
	}

	for _, o := range ops {
		o := o
		for _, ft := range []struct {
			name    string
			typ     wasm.ValType
			arithOp byte
			storeOp byte
			loadOp  byte
			align   byte
		}{
			{"f32", wasm.F32, o.f32Op, 0x38, 0x2a, 0x02},
			{"f64", wasm.F64, o.f64Op, 0x39, 0x2b, 0x03},
		} {
			t.Run(ft.name+"."+o.name, func(t *testing.T) {
				body := build(ft.storeOp, ft.loadOp, ft.arithOp, ft.align)
				m := modFor(t, ft.typ, body)
				for _, in := range inputs {
					a, b := in.a, in.b
					var av, bv uint64
					var want uint64
					if ft.typ == wasm.F32 {
						av, bv = f32b(float32(a)), f32b(float32(b))
						want = f32b(float32(o.fn(float64(float32(a)), float64(float32(b)))))
					} else {
						av, bv = f64b(a), f64b(b)
						want = f64b(o.fn(a, b))
					}
					got := runAmd64u(t, m, av, bv)
					// Compare bit patterns, but treat any NaN as equal to any NaN.
					if !bitsEqualOrNaN(got, want, ft.typ == wasm.F32) {
						t.Fatalf("%s.%s(%v,%v) = %#x, want %#x", ft.name, o.name, a, b, got, want)
					}
				}
			})
		}
	}
}

// bitsEqualOrNaN compares result bit patterns, collapsing all NaNs to equal
// (wasm does not mandate a canonical NaN payload for arithmetic results).
func bitsEqualOrNaN(got, want uint64, f32 bool) bool {
	if got == want {
		return true
	}
	if f32 {
		return math.IsNaN(float64(math.Float32frombits(uint32(got)))) &&
			math.IsNaN(float64(math.Float32frombits(uint32(want))))
	}
	return math.IsNaN(math.Float64frombits(got)) && math.IsNaN(math.Float64frombits(want))
}
