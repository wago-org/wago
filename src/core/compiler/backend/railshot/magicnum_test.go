package railshot

import (
	"math/big"
	"math/bits"
	"math/rand"
	"testing"
)

func TestMagicNumbersMatchBigIntegerReference(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for _, width := range []uint{32, 64} {
		divisors := []uint64{3, 5, 7, 9, 10, 11, 13, 31, 33, 63, 65}
		if width == 32 {
			divisors = append(divisors, 1<<31-1, 1<<31+1, 1<<32-1)
		} else {
			divisors = append(divisors, 1<<63-1, 1<<63+1, 1<<64-1)
		}
		for range 10_000 {
			d := rng.Uint64()
			if width == 32 {
				d = uint64(uint32(d))
			}
			if d >= 2 && d&(d-1) != 0 {
				divisors = append(divisors, d)
			}
		}
		for _, d := range divisors {
			if d >= uint64(1)<<(width-1) {
				gotMagic, gotShift, gotAdd := MagicU(d, width)
				wantMagic, wantShift, wantAdd := referenceMagicU(d, width)
				if gotMagic != wantMagic || gotShift != wantShift || gotAdd != wantAdd {
					t.Fatalf("MagicU(%#x, %d) = %#x/%d/%v, want %#x/%d/%v", d, width, gotMagic, gotShift, gotAdd, wantMagic, wantShift, wantAdd)
				}
				continue
			}

			gotMagic, gotShift, gotAdd := MagicU(d, width)
			wantMagic, wantShift, wantAdd := referenceMagicU(d, width)
			if gotMagic != wantMagic || gotShift != wantShift || gotAdd != wantAdd {
				t.Fatalf("MagicU(%#x, %d) = %#x/%d/%v, want %#x/%d/%v", d, width, gotMagic, gotShift, gotAdd, wantMagic, wantShift, wantAdd)
			}
			gotSignedMagic, gotSignedShift, gotAddN := MagicS(d, width)
			wantSignedMagic, wantSignedShift, wantAddN := referenceMagicS(d, width)
			if gotSignedMagic != wantSignedMagic || gotSignedShift != wantSignedShift || gotAddN != wantAddN {
				t.Fatalf("MagicS(%#x, %d) = %#x/%d/%v, want %#x/%d/%v", d, width, gotSignedMagic, gotSignedShift, gotAddN, wantSignedMagic, wantSignedShift, wantAddN)
			}
		}
	}
}

func referenceMagicU(d uint64, width uint) (magic uint64, shift uint, add bool) {
	one := big.NewInt(1)
	fl := uint(bits.Len64(d)) - 1
	divisor := new(big.Int).SetUint64(d)
	numerator := new(big.Int).Lsh(new(big.Int).Set(one), width+fl)
	proposed, remainder := new(big.Int), new(big.Int)
	proposed.DivMod(numerator, divisor, remainder)
	e := new(big.Int).Sub(divisor, remainder)
	if e.Cmp(new(big.Int).Lsh(new(big.Int).Set(one), fl)) < 0 {
		proposed.Add(proposed, one)
		return referenceTruncW(proposed, width), fl, false
	}
	proposed.Lsh(proposed, 1)
	if new(big.Int).Lsh(remainder, 1).Cmp(divisor) >= 0 {
		proposed.Add(proposed, one)
	}
	proposed.Add(proposed, one)
	return referenceTruncW(proposed, width), fl, true
}

func referenceMagicS(d uint64, width uint) (magic int64, shift uint, addN bool) {
	one := big.NewInt(1)
	fl := uint(bits.Len64(d)) - 1
	divisor := new(big.Int).SetUint64(d)
	numerator := new(big.Int).Lsh(new(big.Int).Set(one), width-1+fl)
	proposed, remainder := new(big.Int), new(big.Int)
	proposed.DivMod(numerator, divisor, remainder)
	e := new(big.Int).Sub(divisor, remainder)
	if e.Cmp(new(big.Int).Lsh(new(big.Int).Set(one), fl)) < 0 {
		proposed.Add(proposed, one)
		return signW(referenceTruncW(proposed, width), width), fl - 1, false
	}
	proposed.Lsh(proposed, 1)
	if new(big.Int).Lsh(remainder, 1).Cmp(divisor) >= 0 {
		proposed.Add(proposed, one)
	}
	proposed.Add(proposed, one)
	return signW(referenceTruncW(proposed, width), width), fl, true
}

func referenceTruncW(v *big.Int, width uint) uint64 {
	if width >= 64 {
		return v.Uint64()
	}
	return v.Uint64() & (uint64(1)<<width - 1)
}
