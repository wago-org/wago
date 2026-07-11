//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// extractLane0 wraps expr (which must leave a v128 on the stack) in
// i32x4.extract_lane 0.
func extractLane0(expr []byte) []byte {
	out := append([]byte{}, expr...)
	out = append(out, simdOp(27)...) // i32x4.extract_lane
	out = append(out, 0x00)          // lane 0
	return out
}

// TestSIMDV128ConstCacheExec guards the reserved-register v128 constant cache
// (preloadV128Consts / v128ConstReg copy-from-cache). A loop-invariant v128.const
// that appears more than once is materialized once into a reserved V register, and
// each use is a MOV.16b copy of that register. The reserved register must never be
// written or handed out by the allocator.
//
// The key hazard: a destructive op (here i8x16.add whose result is consumed by
// extract_lane, so it reuses an operand register as its destination) writes the
// materialized copy. If v128ConstReg mistakenly returned the reserved register
// itself instead of a fresh copy — or the allocator handed the reserved register
// out as scratch — that write would corrupt the cached constant, and a later use of
// the same const would read the wrong bits. Each case recomputes the exact expected
// bytes in Go so any corruption is caught.
func TestSIMDV128ConstCacheExec(t *testing.T) {
	K := [16]byte{0x78, 0x56, 0x34, 0x12, 0x98, 0xBA, 0xDC, 0xFE, 0x5F, 0x13, 0x59, 0x13, 0xE0, 0xAC, 0x68, 0x24}
	M := [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}

	addKK := func() []byte { // i8x16.add (v128.const K)(v128.const K) -> destructive, reuses a copy
		b := append([]byte{}, simdConst(K)...)
		b = append(b, simdConst(K)...)
		b = append(b, simdOp(110)...) // i8x16.add
		return b
	}

	// A destructive add on copies of K (result reused via extract), THEN a plain read
	// of K. The second read must still see K, not the add's output. K appears 3x, so
	// it is cached. Returns extract0(add) + extract0(K).
	t.Run("read-after-destructive", func(t *testing.T) {
		var b []byte
		b = append(b, 0x00) // no locals
		b = append(b, extractLane0(addKK())...)
		b = append(b, extractLane0(simdConst(K))...)
		b = append(b, 0x6a) // i32.add
		b = append(b, 0x0b) // end

		var dbl, single [4]byte
		for i := 0; i < 4; i++ {
			dbl[i] = byte(uint32(K[i]) * 2)
			single[i] = K[i]
		}
		want := binary.LittleEndian.Uint32(dbl[:]) + binary.LittleEndian.Uint32(single[:])
		if got := runArm64I32(t, b); got != want {
			t.Fatalf("read-after-destructive: got %#08x want %#08x (cached const likely corrupted)", got, want)
		}
	})

	// Two distinct cached consts (exercises maxV128Consts=2 and that the two reserved
	// registers never alias each other or a destructive temp). Destructive add on K,
	// then reads of both K and M. Returns extract0(add K K) + extract0(K) + extract0(M).
	t.Run("two-consts", func(t *testing.T) {
		// Force both K and M to be cached by using each twice, plus the destructive use.
		var b []byte
		b = append(b, 0x00) // no locals
		b = append(b, extractLane0(addKK())...)
		b = append(b, extractLane0(simdConst(K))...)
		b = append(b, 0x6a) // + K
		b = append(b, extractLane0(simdConst(M))...)
		b = append(b, 0x6a) // + M
		b = append(b, extractLane0(simdConst(M))...)
		b = append(b, 0x6a) // + M again (so M appears >=2 times -> cached)
		b = append(b, 0x0b) // end

		var dbl, kb, mb [4]byte
		for i := 0; i < 4; i++ {
			dbl[i] = byte(uint32(K[i]) * 2)
			kb[i] = K[i]
			mb[i] = M[i]
		}
		want := binary.LittleEndian.Uint32(dbl[:]) +
			binary.LittleEndian.Uint32(kb[:]) +
			2*binary.LittleEndian.Uint32(mb[:])
		if got := runArm64I32(t, b); got != want {
			t.Fatalf("two-consts: got %#08x want %#08x (cached const likely corrupted)", got, want)
		}
	})
}

var _ = wasm.I32
