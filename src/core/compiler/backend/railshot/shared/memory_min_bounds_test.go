package shared

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"math/rand"
	"testing"
)

func TestUnsignedOperationMaximumContainsConcreteResults(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for _, wide := range []bool{false, true} {
		limit := uint64(^uint32(0))
		mask := uint64(31)
		if wide {
			limit = ^uint64(0)
			mask = 63
		}
		for _, op := range []IntOp{IntAnd, IntOr, IntXor, IntAdd, IntMul, IntShl, IntShrU} {
			for i := 0; i < 4096; i++ {
				aMax, bMax := rng.Uint64()&limit, rng.Uint64()&limit
				if i%3 == 0 {
					aMax &= 65535
					bMax &= 65535
				}
				a, b := rng.Uint64()&limit, rng.Uint64()&limit
				if aMax != limit {
					a %= aMax + 1
				}
				if bMax != limit {
					b %= bMax + 1
				}
				known := i%2 == 0
				if known {
					b = bMax
				}
				bound := UnsignedOperationMaximum(op, aMax, bMax, b, known, wide)
				var value uint64
				switch op {
				case IntAnd:
					value = a & b
				case IntOr:
					value = a | b
				case IntXor:
					value = a ^ b
				case IntAdd:
					value = a + b
				case IntMul:
					value = a * b
				case IntShl:
					value = a << (b & mask)
				case IntShrU:
					value = a >> (b & mask)
				}
				value &= limit
				if value > bound {
					t.Fatalf("wide=%t op=%d max=(%x,%x) inputs=(%x,%x) known=%t: value=%x exceeds %x", wide, op, aMax, bMax, a, b, known, value, bound)
				}
			}
		}
	}
	// Numeric interval maxima cannot be used directly as bit masks.
	if got := UnsignedOperationMaximum(IntOr, 5, 4, 0, false, false); got < 3|4 {
		t.Fatalf("OR interval bound=%d", got)
	}
}

func TestProvenMinimumAccessChecksEntireRangeAndOverflow(t *testing.T) {
	for _, wide := range []bool{false, true} {
		mt := wasm.MemType{Limits: wasm.Limits{Min: 1, Addr64: wide}}
		for _, tc := range []struct {
			max, offset uint64
			size        int
			want        bool
		}{
			{32765, 0, 16, true}, {65520, 0, 16, true}, {65521, 0, 16, false},
			{0, 65532, 4, true}, {1, 65532, 4, false}, {0, ^uint64(0), 1, false},
			{^uint64(0), 1, 1, false}, {0, 0, 0, false}, {0, 0, -1, false},
		} {
			if got := ProvenMinimumAccess(mt, tc.max, tc.offset, tc.size); got != tc.want {
				t.Fatalf("wide=%t case=%+v: %t", wide, tc, got)
			}
		}
		mt.Limits.Min = 0
		if ProvenMinimumAccess(mt, 0, 0, 1) {
			t.Fatal("zero minimum proves no access")
		}
		mt.Limits.Min = 1 << 48
		if ProvenMinimumAccess(mt, 0, 0, 1) {
			t.Fatal("page-byte conversion wrapped")
		}
	}
	mt := wasm.MemType{Limits: wasm.Limits{Min: 65536}}
	if !ProvenMinimumAccess(mt, uint64(^uint32(0)), 0, 1) {
		t.Fatal("last memory32 byte should fit full minimum")
	}
}
