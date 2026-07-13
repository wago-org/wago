//go:build arm64

package arm64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// TestMulAddFuse checks add(c,a*b)→MADD / sub(c,a*b)→MSUB fusion (and that the
// non-fusable a*b-c form still computes correctly), for i32 and i64, against Go.
func TestMulAddFuse(t *testing.T) {
	// body builders: params (x,y,z) of type T, result T.
	// mul/add/sub opcodes per width.
	type widthOps struct {
		typ           wasm.ValType
		lget          func(i byte) []byte
		mul, add, sub byte
		fold          func(uint64) uint64 // canonicalize to the width
	}
	i32ops := widthOps{wasm.I32, func(i byte) []byte { return []byte{0x20, i} }, 0x6c, 0x6a, 0x6b,
		func(v uint64) uint64 { return uint64(uint32(v)) }}
	i64ops := widthOps{wasm.I64, func(i byte) []byte { return []byte{0x20, i} }, 0x7e, 0x7c, 0x7d,
		func(v uint64) uint64 { return v }}

	// shapes: (name, body(ops), expected(a,b,c))
	shapes := []struct {
		name   string
		body   func(w widthOps) []byte
		expect func(a, b, c uint64) uint64
	}{
		{"a*b+c", func(w widthOps) []byte { // add(mul(a,b), c)
			return concat(w.lget(0), w.lget(1), []byte{w.mul}, w.lget(2), []byte{w.add, 0x0b})
		}, func(a, b, c uint64) uint64 { return a*b + c }},
		{"c+a*b", func(w widthOps) []byte { // add(c, mul(a,b))
			return concat(w.lget(2), w.lget(0), w.lget(1), []byte{w.mul, w.add, 0x0b})
		}, func(a, b, c uint64) uint64 { return c + a*b }},
		{"c-a*b", func(w widthOps) []byte { // sub(c, mul(a,b)) → MSUB
			return concat(w.lget(2), w.lget(0), w.lget(1), []byte{w.mul, w.sub, 0x0b})
		}, func(a, b, c uint64) uint64 { return c - a*b }},
		{"a*b-c", func(w widthOps) []byte { // sub(mul(a,b), c) → NOT MSUB (fallback)
			return concat(w.lget(0), w.lget(1), []byte{w.mul}, w.lget(2), []byte{w.sub, 0x0b})
		}, func(a, b, c uint64) uint64 { return a*b - c }},
	}
	inputs := []struct{ a, b, c uint64 }{
		{3, 4, 5}, {7, 8, 100}, {0, 9, 3}, {1, 1, 1},
		{0xFFFFFFFF, 2, 1}, {0x1_0000_0000, 3, 7}, {12345, 6789, 999},
	}

	for _, w := range []widthOps{i32ops, i64ops} {
		wname := "i32"
		if w.typ == wasm.I64 {
			wname = "i64"
		}
		for _, sh := range shapes {
			body := append([]byte{0x00}, sh.body(w)...) // 0 locals prefix
			t.Run(wname+"/"+sh.name, func(t *testing.T) {
				m := mod1(t, []wasm.ValType{w.typ, w.typ, w.typ}, []wasm.ValType{w.typ}, body)
				for _, in := range inputs {
					got := w.fold(runArm64u(t, m, in.a, in.b, in.c))
					want := w.fold(sh.expect(in.a, in.b, in.c))
					if got != want {
						t.Fatalf("%s %s(a=%d,b=%d,c=%d) = %d, want %d", wname, sh.name, in.a, in.b, in.c, got, want)
					}
				}
			})
		}
	}
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
