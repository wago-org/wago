//go:build linux && amd64

package amd64

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/runtime"
)

// Unsigned i64→float must treat the top bit as magnitude, not sign: a signed
// cvtsi2sd is wrong for values >= 2^63.
func TestConvertI64UToFloat(t *testing.T) {
	run := func(op, resTy string, arg uint64) uint64 {
		m := watToModule(t, "(module (func (export \"f\") (param i64) (result "+resTy+") local.get 0 "+op+"))")
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
		sa, rs, tp := ar.Alloc(64), ar.Alloc(16), ar.Alloc(8)
		binary.LittleEndian.PutUint64(sa, arg)
		if err := eng.Call(entry, sa, jm.LinearMemory(), tp, rs); err != nil {
			t.Fatalf("call: %v", err)
		}
		return binary.LittleEndian.Uint64(rs)
	}

	cases := []uint64{
		0,
		1,
		1 << 62,
		1 << 63,            // 2^63: signed cvt would yield a negative
		0x8000000000000001, // just above 2^63
		0xFFFFFFFFFFFFFFFF, // 2^64-1
		0xFFFFFFFFFFFFFC00, // exercises rounding near the top
	}
	for _, v := range cases {
		gotD := math.Float64frombits(run("f64.convert_i64_u", "f64", v))
		if want := float64(v); gotD != want {
			t.Errorf("f64.convert_i64_u(%#x) = %v, want %v", v, gotD, want)
		}
		gotS := math.Float32frombits(uint32(run("f32.convert_i64_u", "f32", v)))
		if want := float32(v); gotS != want {
			t.Errorf("f32.convert_i64_u(%#x) = %v, want %v", v, gotS, want)
		}
	}
}
