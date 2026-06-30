//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/runtime"
)

// runTrunc compiles a single function (float param -> int result) and runs it,
// returning the raw result and whether it trapped.
func runTrunc(t *testing.T, wat string, argBits uint64) (uint64, bool) {
	t.Helper()
	m := watToModule(t, wat)
	code, err := CompileFunction(m, 0)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	eng, _ := runtime.NewEngine()
	defer eng.Close()
	jm, _ := runtime.NewJobMemory(1 << 16)
	defer jm.Close()
	ar, _ := runtime.NewArena(4096)
	defer ar.Close()
	mem, entry, _ := runtime.MapCode(code)
	defer runtime.Unmap(mem)
	serArgs := ar.Alloc(64)
	results := ar.Alloc(16)
	trap := ar.Alloc(8)
	binary.LittleEndian.PutUint64(serArgs, argBits)
	if err := eng.Call(entry, serArgs, jm.LinearMemory(), trap, results); err != nil {
		return 0, true
	}
	return binary.LittleEndian.Uint64(results), false
}

func f64mod(op string) string {
	return `(module (func (export "f") (param f64) (result ` + resultType(op) + `) local.get 0 ` + op + `))`
}
func f32mod(op string) string {
	return `(module (func (export "f") (param f32) (result ` + resultType(op) + `) local.get 0 ` + op + `))`
}
func resultType(op string) string {
	if op[:3] == "i64" {
		return "i64"
	}
	return "i32"
}

func TestTruncInRange(t *testing.T) {
	cases := []struct {
		op   string
		arg  float64
		want uint64
	}{
		{"i32.trunc_f64_s", 15.9, 15},
		{"i32.trunc_f64_s", -15.9, uint64(uint32(0xFFFFFFF1))}, // -15
		{"i32.trunc_f64_u", 3000000000.5, 3000000000},
		{"i64.trunc_f64_u", 9.5e18, uint64(9.5e18)},       // < 2^63 (~9.22e18)
		{"i64.trunc_f64_u", 1.0e19, 10000000000000000000}, // >= 2^63 — the biased path (was buggy)
		{"i64.trunc_f64_u", 1.8e19, uint64(1.8e19)},       // near 2^64
	}
	for _, c := range cases {
		got, trapped := runTrunc(t, f64mod(c.op), math.Float64bits(c.arg))
		if trapped {
			t.Errorf("%s(%g) trapped, want %d", c.op, c.arg, c.want)
		} else if got != c.want {
			t.Errorf("%s(%g) = %d, want %d", c.op, c.arg, got, c.want)
		}
	}
	// Negative signed i64 (runtime conversion to avoid a const-overflow literal).
	neg := int64(-1234567890123)
	if got, trapped := runTrunc(t, f64mod("i64.trunc_f64_s"), math.Float64bits(float64(neg))); trapped || got != uint64(neg) {
		t.Errorf("i64.trunc_f64_s(%d) = %d trapped=%v, want %d", neg, got, trapped, uint64(neg))
	}
	// f32 sanity: large unsigned i32.
	if got, trapped := runTrunc(t, f32mod("i32.trunc_f32_u"), uint64(math.Float32bits(4000000000.0))); trapped || got != 4000000000 {
		t.Errorf("i32.trunc_f32_u(4e9) = %d trapped=%v, want 4000000000", got, trapped)
	}
}

func TestTruncTraps(t *testing.T) {
	nan := math.Float64bits(math.NaN())
	inf := math.Float64bits(math.Inf(1))
	cases := []struct {
		op  string
		arg uint64
	}{
		{"i32.trunc_f64_s", nan},
		{"i64.trunc_f64_u", nan},
		{"i32.trunc_f64_s", inf},
		{"i32.trunc_f64_s", math.Float64bits(3e9)},  // > i32 max
		{"i32.trunc_f64_s", math.Float64bits(-3e9)}, // < i32 min
		{"i32.trunc_f64_u", math.Float64bits(-1.0)}, // u: <= -1 traps
		{"i32.trunc_f64_u", math.Float64bits(5e9)},  // > u32 max
		{"i64.trunc_f64_s", math.Float64bits(1e19)}, // > i64 max
		{"i64.trunc_f64_u", math.Float64bits(2e19)}, // > u64 max
		{"i64.trunc_f64_u", math.Float64bits(-1.0)}, // u: <= -1 traps
	}
	for _, c := range cases {
		if _, trapped := runTrunc(t, f64mod(c.op), c.arg); !trapped {
			t.Errorf("%s(0x%x) did not trap, but should", c.op, c.arg)
		}
	}
	// Just-in-range boundaries must NOT trap.
	if _, trapped := runTrunc(t, f64mod("i32.trunc_f64_u"), math.Float64bits(-0.9)); trapped {
		t.Error("i32.trunc_f64_u(-0.9) trapped; should be 0")
	}
}
