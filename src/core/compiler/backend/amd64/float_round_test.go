//go:build linux && amd64

package amd64

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
)

// runF32Raw compiles function 0, passes one f32 argument (raw bits), and returns
// the f32 result's raw bits — for sign/NaN-payload checks that float equality
// would hide.
func runF32Raw(t *testing.T, m *wasm.Module, argBits uint32) uint32 {
	t.Helper()
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
	binary.LittleEndian.PutUint32(serArgs, argBits)
	if err := eng.Call(entry, serArgs, jm.LinearMemory(), trap, results); err != nil {
		t.Fatalf("call: %v", err)
	}
	return binary.LittleEndian.Uint32(results)
}

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

// TestFloatRoundingSignedZero checks raw bits (not float ==) so it distinguishes
// +0 from -0, which the rounding ops and copysign must preserve. SSE path.
func TestFloatRoundingSignedZero(t *testing.T) {
	const negZero = uint64(0x8000000000000000)
	cases := []struct {
		name string
		body string
		a    float64
		want uint64
	}{
		{"f64.trunc(-0.3)", `local.get 0 f64.trunc`, -0.3, negZero},
		{"f64.ceil(-0.3)", `local.get 0 f64.ceil`, -0.3, negZero},
		{"f64.nearest(-0.5)", `local.get 0 f64.nearest`, -0.5, negZero},
		{"f64.copysign(+0,-1)", `local.get 0 f64.const -1.0 f64.copysign`, 0.0, negZero},
		{"f64.copysign(-0,+1)", `local.get 0 f64.const 1.0 f64.copysign`, math.Copysign(0, -1), 0},
		// f32 paths (via demote/promote); the rounded -0 round-trips exactly.
		{"f32.trunc(-0.3)", `local.get 0 f32.demote_f64 f32.trunc f64.promote_f32`, -0.3, negZero},
		{"f32.ceil(-0.3)", `local.get 0 f32.demote_f64 f32.ceil f64.promote_f32`, -0.3, negZero},
		{"f32.copysign(+0,-1)", `local.get 0 f32.demote_f64 f32.const -1.0 f32.copysign f64.promote_f32`, 0.0, negZero},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := runF64Raw(t, f64fn(t, c.body), c.a, 0); got != c.want {
				t.Fatalf("%s = %#016x, want %#016x", c.name, got, c.want)
			}
		})
	}
}

// TestFloatCopysignNaN checks copysign keeps the first operand's magnitude and
// NaN payload and takes only the second operand's sign bit. SSE path.
func TestFloatCopysignNaN(t *testing.T) {
	// f64: NaN payload 0x...1234, sign from -0.0 -> negative NaN, same payload.
	got := runF64Raw(t, f64fn(t, `local.get 0 f64.const -0.0 f64.copysign`),
		math.Float64frombits(0x7ff8000000001234), 0)
	if got != 0xfff8000000001234 {
		t.Fatalf("f64 copysign(NaN payload, -0.0) = %#016x, want 0xfff8000000001234", got)
	}
	// f32 version (direct, so the payload isn't reshaped by demote/promote).
	m32 := watToModule(t, `(module (func (export "f") (param f32) (result f32)
		local.get 0 f32.const -0.0 f32.copysign))`)
	if g32 := runF32Raw(t, m32, 0x7fc01234); g32 != 0xffc01234 {
		t.Fatalf("f32 copysign(NaN payload, -0.0) = %#08x, want 0xffc01234", g32)
	}
}

// TestFloatCopysignFold exercises the const-const fold path (bit-exact, including
// signed zero).
func TestFloatCopysignFold(t *testing.T) {
	cases := []struct {
		name string
		body string
		want uint64
	}{
		{"fold +0<-(-1)", `f64.const 0.0 f64.const -1.0 f64.copysign`, 0x8000000000000000},
		{"fold -0<-(+1)", `f64.const -0.0 f64.const 1.0 f64.copysign`, 0},
		{"fold 3<-(-1)", `f64.const 3.0 f64.const -1.0 f64.copysign`, math.Float64bits(-3.0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := runF64Raw(t, f64fn(t, c.body), 0, 0); got != c.want {
				t.Fatalf("%s = %#016x, want %#016x", c.name, got, c.want)
			}
		})
	}
}

// TestRoundEncoding byte-checks the ROUNDSS/ROUNDSD encoder, including the REX
// branch for high (XMM8+) registers that the WAT tests don't reach. Reg values
// 8..11 are used as XMM register indices here.
func TestRoundEncoding(t *testing.T) {
	cases := []struct {
		name     string
		dst, src Reg
		f64      bool
		mode     byte
		want     []byte
	}{
		{"roundss low (no REX)", 0, 1, false, roundNearest, []byte{0x66, 0x0F, 0x3A, 0x0A, 0xC1, 0x08}},
		{"roundss high (REX.RB)", R8, R9, false, roundCeil, []byte{0x66, 0x45, 0x0F, 0x3A, 0x0A, 0xC1, 0x0A}},
		{"roundsd high (REX.RB)", R10, R11, true, roundFloor, []byte{0x66, 0x45, 0x0F, 0x3A, 0x0B, 0xD3, 0x09}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := &Asm{}
			a.Round(c.dst, c.src, c.f64, c.mode)
			if !bytes.Equal(a.B, c.want) {
				t.Fatalf("%s = % x, want % x", c.name, a.B, c.want)
			}
		})
	}
}
