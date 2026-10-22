//go:build linux && !tinygo

package riscv64

import (
	"strings"
	"testing"

	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func swarPermutationFixture(shuffle bool, indices [16]byte) fixtureFunc {
	f := fixtureFunc{params: []byte{0x7b, 0x7b}, result: 0x7b, body: []byte{0x20, 0, 0x20, 1, 0xfd, 14}}
	if shuffle {
		f.body[5] = 13
		f.body = append(f.body, indices[:]...)
	}
	f.body = append(f.body, 0x0b)
	return f
}

func TestSWARBytePermutationAdmissionAndImmediates(t *testing.T) {
	for _, shuffle := range []bool{false, true} {
		var indices [16]byte
		for i := range indices {
			indices[i] = byte(31 - i)
		}
		fn, ft := swarValidatedFunction(t, swarPermutationFixture(shuffle, indices))
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); len(code) == 0 || err != nil {
			t.Fatal("valid byte permutation", shuffle, err)
		}
		for _, prefix := range [][]byte{nil, {0x20, 0}, {0x20, 0, 0x41, 0}, {0x41, 0, 0x20, 0}} {
			bad := *fn
			bad.BodyBytes = append(append([]byte(nil), prefix...), fn.BodyBytes[4:]...)
			if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("invalid permutation carriers emitted", shuffle, prefix, err)
			}
		}
		if shuffle {
			for _, index := range []byte{32, 255} {
				for lane := range indices {
					bad := *fn
					bad.BodyBytes = append([]byte(nil), fn.BodyBytes...)
					bad.BodyBytes[6+lane] = index
					if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
						t.Fatal("out-of-range shuffle immediate emitted", lane, index, err)
					}
				}
			}
			for size := 0; size < 16; size++ {
				bad := *fn
				bad.BodyBytes = bad.BodyBytes[:6+size]
				if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
					t.Fatal("truncated shuffle literal emitted", size, err)
				}
			}
		}
	}
	for _, index := range []byte{32, 255} {
		f := integerEmitter{}
		f.a.Addi(enc.X5, enc.Zero, 1)
		before := len(f.a.B)
		var indices [16]byte
		indices[15] = index
		f.swarShuffle(indices)
		if f.err == nil || len(f.a.B) != before {
			t.Fatal("invalid shuffle helper partially emitted", index, f.err)
		}
		f.swarSwizzle()
		if len(f.a.B) != before {
			t.Fatal("sticky permutation error appended bytes")
		}
	}
}

func TestSWARBytePermutationCodeBudgetBeforeEmission(t *testing.T) {
	// The admitted conservative bound includes the frame budget plus the
	// initial local.get and each subsequent local.get/swizzle pair. This limit
	// arrives before the instruction, source-byte or operand-stack bounds.
	maxPairs := (MaxCodeBytes - (64 << 10) - 256) / (256 + 2048)
	for _, count := range []int{maxPairs, maxPairs + 1} {
		fixture := fixtureFunc{params: []byte{0x7b, 0x7b}, result: 0x7b, body: []byte{0x20, 0}}
		for i := 0; i < count; i++ {
			fixture.body = append(fixture.body, 0x20, 1, 0xfd, 14)
		}
		fixture.body = append(fixture.body, 0x0b)
		fn, ft := swarValidatedFunction(t, fixture)
		summary, err := walkSWARPairs(fn, ft, false, nil)
		if count == maxPairs {
			if err != nil || summary.codeBound > MaxCodeBytes || summary.peak != 2 {
				t.Fatal("exact conservative code budget admission", summary, err)
			}
		} else {
			if err == nil || !strings.Contains(err.Error(), "code budget") {
				t.Fatal("code budget overflow admitted", err)
			}
			if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); code != nil || err == nil || !strings.Contains(err.Error(), "code budget") {
				t.Fatal("oversize function returned an image", len(code), err)
			}
		}
	}
}
