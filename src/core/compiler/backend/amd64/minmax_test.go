//go:build linux && amd64

package amd64

import (
	"math"
	"testing"
)

// f64.min/max must be sign-correct on zeros (min keeps -0, max keeps +0) and
// propagate NaN — none of which x86 minsd/maxsd do on their own.
func TestF64MinMaxSignedZeroAndNaN(t *testing.T) {
	negZero := math.Copysign(0, -1)
	posZero := 0.0
	nz := math.Float64bits(negZero) // 0x8000000000000000
	pz := math.Float64bits(posZero) // 0

	cases := []struct {
		name string
		op   string
		a, b float64
		want uint64
	}{
		{"min(-0,+0)", "f64.min", negZero, posZero, nz},
		{"min(+0,-0)", "f64.min", posZero, negZero, nz},
		{"min(-0,-0)", "f64.min", negZero, negZero, nz},
		{"min(+0,+0)", "f64.min", posZero, posZero, pz},
		{"max(-0,+0)", "f64.max", negZero, posZero, pz},
		{"max(+0,-0)", "f64.max", posZero, negZero, pz},
		{"max(-0,-0)", "f64.max", negZero, negZero, nz},
		{"max(+0,+0)", "f64.max", posZero, posZero, pz},
		{"min(3,5)", "f64.min", 3, 5, math.Float64bits(3)},
		{"max(3,5)", "f64.max", 3, 5, math.Float64bits(5)},
		{"min(-2,-9)", "f64.min", -2, -9, math.Float64bits(-9)},
		{"max(-2,-9)", "f64.max", -2, -9, math.Float64bits(-2)},
	}
	for _, c := range cases {
		got := runF64Raw(t, f64fn(t, "local.get 0 local.get 1 "+c.op), c.a, c.b)
		if got != c.want {
			t.Errorf("%s = %#016x, want %#016x", c.name, got, c.want)
		}
	}

	// NaN on either side propagates a NaN.
	for _, c := range []struct {
		op   string
		a, b float64
	}{
		{"f64.min", math.NaN(), 1}, {"f64.min", 1, math.NaN()},
		{"f64.max", math.NaN(), 1}, {"f64.max", 1, math.NaN()},
	} {
		got := runF64Raw(t, f64fn(t, "local.get 0 local.get 1 "+c.op), c.a, c.b)
		if !math.IsNaN(math.Float64frombits(got)) {
			t.Errorf("%s with NaN = %#016x, want a NaN", c.op, got)
		}
	}
}
