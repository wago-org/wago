//go:build linux && amd64

package amd64

import (
	"math"
	"testing"
)

// TestFloatRounding covers f64.ceil/floor/trunc/nearest (ROUNDSD modes).
func TestFloatRounding(t *testing.T) {
	cases := []struct {
		name string
		body string
		a    float64
		want float64
	}{
		{"ceil/up", `local.get 0 f64.ceil`, 2.3, 3},
		{"ceil/neg", `local.get 0 f64.ceil`, -2.3, -2},
		{"floor/down", `local.get 0 f64.floor`, 2.7, 2},
		{"floor/neg", `local.get 0 f64.floor`, -2.3, -3},
		{"trunc/pos", `local.get 0 f64.trunc`, 2.7, 2},
		{"trunc/neg", `local.get 0 f64.trunc`, -2.7, -2},
		{"nearest/down", `local.get 0 f64.nearest`, 2.4, 2},
		{"nearest/up", `local.get 0 f64.nearest`, 2.6, 3},
		{"nearest/tie-even-2.5", `local.get 0 f64.nearest`, 2.5, 2}, // ties to even
		{"nearest/tie-even-3.5", `local.get 0 f64.nearest`, 3.5, 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := math.Float64frombits(runF64Raw(t, f64fn(t, c.body), c.a, 0))
			if got != c.want {
				t.Fatalf("%s(%v) = %v, want %v", c.body, c.a, got, c.want)
			}
		})
	}
}

// TestFloatCopysign covers f64.copysign, including a negative-zero sign source.
func TestFloatCopysign(t *testing.T) {
	cases := []struct {
		name string
		a, b float64
		want float64
	}{
		{"pos<-neg", 3.0, -1.0, -3.0},
		{"neg<-pos", -3.0, 1.0, 3.0},
		{"pos<-neg0", 3.0, math.Copysign(0, -1), -3.0}, // sign of -0.0
		{"big<-pos", -5.0, 2.0, 5.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := math.Float64frombits(runF64Raw(t, f64fn(t, `local.get 0 local.get 1 f64.copysign`), c.a, c.b))
			if got != c.want {
				t.Fatalf("copysign(%v,%v) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

// TestFloatF32Ops drives the f32 ROUNDSS / andps / orps paths via demote.
func TestFloatF32Ops(t *testing.T) {
	cases := []struct {
		name string
		body string
		a, b float64
		want float64
	}{
		{"f32.ceil", `local.get 0 f32.demote_f64 f32.ceil f64.promote_f32`, 2.3, 0, 3},
		{"f32.floor", `local.get 0 f32.demote_f64 f32.floor f64.promote_f32`, 2.7, 0, 2},
		{"f32.trunc", `local.get 0 f32.demote_f64 f32.trunc f64.promote_f32`, -2.7, 0, -2},
		{"f32.nearest", `local.get 0 f32.demote_f64 f32.nearest f64.promote_f32`, 2.5, 0, 2},
		{"f32.copysign", `local.get 0 f32.demote_f64 local.get 1 f32.demote_f64 f32.copysign f64.promote_f32`, 3.0, -1.0, -3.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := math.Float64frombits(runF64Raw(t, f64fn(t, c.body), c.a, c.b))
			if got != c.want {
				t.Fatalf("%s(%v,%v) = %v, want %v", c.name, c.a, c.b, got, c.want)
			}
		})
	}
}
