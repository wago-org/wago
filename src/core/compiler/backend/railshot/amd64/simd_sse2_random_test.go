//go:build linux && amd64 && !tinygo

package amd64

import (
	"encoding/binary"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math/rand"
	"testing"
)

func TestSSE2SIMDRandomized(t *testing.T) {
	rng := rand.New(rand.NewSource(731))
	// Exercise optional-instruction fallback families through their real Wasm
	// lowering. Existing lane oracles and the official suites provide independent
	// reference semantics; this adds deterministic modern/baseline differentials.
	for iteration := 0; iteration < 32; iteration++ {
		var a, b [16]byte
		rng.Read(a[:])
		rng.Read(b[:])
		if iteration&1 == 0 {
			copy(b[4:8], a[4:8])
			copy(b[12:16], a[12:16])
		}
		for _, op := range []uint32{14, 110, 118, 130, 142, 149, 174, 181, 182, 183, 184, 185, 213, 214, 215, 216, 217, 218, 219, 273, 274} {
			m := mod1(t, nil, []wasm.ValType{wasm.V128}, v128BinaryBody(a, b, op))
			runAmd64V128(t, m, nil)
		}
		for _, op := range []uint32{96, 128, 160, 103, 104, 105, 106, 116, 117, 122, 148} {
			body := append([]byte{0}, v128ConstBytes(a)...)
			body = append(body, simdOp(op)...)
			body = append(body, 0x0b)
			runAmd64V128(t, mod1(t, nil, []wasm.ValType{wasm.V128}, body), nil)
		}
	}
	// Every byte index must be tested, including the relaxed raw-PSHUFB cases.
	var a, b [16]byte
	for i := range a {
		a[i] = byte(i + 1)
	}
	for start := 0; start < 256; start += 16 {
		for i := range b {
			b[i] = byte(start + i)
		}
		for _, op := range []uint32{14, 256} {
			got := runAmd64V128(t, mod1(t, nil, []wasm.ValType{wasm.V128}, v128BinaryBody(a, b, op)), nil)
			for i, index := range b {
				want := byte(0)
				if op == 14 && index < 16 {
					want = a[index]
				}
				if op == 256 && index < 128 {
					want = a[index&15]
				}
				if got[i] != want {
					t.Fatalf("shuffle opcode=%d index=%d got=%d want=%d", op, index, got[i], want)
				}
			}
		}
	}
	// Equal high words with low-word unsigned ordering: signed qword comparison
	// must not accidentally compare the low words as signed dwords.
	binary.LittleEndian.PutUint64(a[:], 0x00000000ffffffff)
	binary.LittleEndian.PutUint64(b[:], 0x0000000000000001)
	got := runAmd64V128(t, mod1(t, nil, []wasm.ValType{wasm.V128}, v128BinaryBody(a, b, 217)), nil)
	if binary.LittleEndian.Uint64(got[:]) != ^uint64(0) {
		t.Fatal("qword low-word tie break failed")
	}
}
