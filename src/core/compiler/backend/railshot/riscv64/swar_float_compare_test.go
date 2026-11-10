//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func TestSWARRawFPTransportRemainsGPOnlyInIsolatedMode(t *testing.T) {
	fixtures := []fixtureFunc{swarScalarConstantFixture(0x7d, 0x7f800001), swarScalarConstantFixture(0x7c, 0x7ff0000000000001)}
	for _, kind := range []uint32{19, 20, 31, 32, 33, 34} {
		lanes := byte(1)
		if kind >= 31 {
			lanes = 2
			if kind <= 32 {
				lanes = 4
			}
		}
		for lane := byte(0); lane < lanes; lane++ {
			fixtures = append(fixtures, swarWideLaneFixture(kind, lane))
		}
	}
	for _, fixture := range fixtures {
		fn, ft := swarValidatedFunction(t, fixture)
		summary, err := walkSWARPairs(fn, ft, false, nil)
		if err != nil || summary.usesFP {
			t.Fatal("raw FP transport marked arithmetic", fixture, summary, err)
		}
		code, _, err := compileSWARPairs(fn, ft, emissionMode{})
		if err != nil || len(code) == 0 {
			t.Fatal("isolated raw transport rejected", fixture, err)
		}
		for offset := 0; offset < len(code); offset += 4 {
			switch binary.LittleEndian.Uint32(code[offset:]) & 0x7f {
			case 0x07, 0x27, 0x43, 0x47, 0x4b, 0x4f, 0x53:
				t.Fatal("raw transport emitted FP instruction", fixture, offset)
			}
		}
	}
}

// The oracle compares IEEE bit patterns without host FP operations, avoiding
// hardware comparison instructions or ambient exception flags in the oracle.
func swarFloatComparisonOracle(a, b uint64, width int, relation int) bool {
	sign, exponent, fraction, mask := uint64(0x80000000), uint64(0x7f800000), uint64(0x7fffff), uint64(0xffffffff)
	if width == 64 {
		sign, exponent, fraction, mask = 0x8000000000000000, 0x7ff0000000000000, 0xfffffffffffff, ^uint64(0)
	}
	a &= mask
	b &= mask
	if (a&exponent == exponent && a&fraction != 0) || (b&exponent == exponent && b&fraction != 0) {
		return relation == 1
	}
	if a&^sign == 0 {
		a = 0
	}
	if b&^sign == 0 {
		b = 0
	}
	key := func(value uint64) uint64 {
		if value&sign != 0 {
			return ^value & mask
		}
		return value | sign
	}
	switch relation {
	case 0:
		return a == b
	case 1:
		return a != b
	case 2:
		return key(a) < key(b)
	case 3:
		return key(a) > key(b)
	case 4:
		return key(a) <= key(b)
	case 5:
		return key(a) >= key(b)
	default:
		panic("invalid comparison oracle")
	}
}

func TestSWARFloatComparisonOracleIEEEEdges(t *testing.T) {
	for _, width := range []int{32, 64} {
		sign, one, inf, nan, snan, max := uint64(0x80000000), uint64(0x3f800000), uint64(0x7f800000), uint64(0x7fc00001), uint64(0x7f800001), uint64(0x7f7fffff)
		if width == 64 {
			sign, one, inf, nan, snan, max = 0x8000000000000000, 0x3ff0000000000000, 0x7ff0000000000000, 0x7ff8000000000001, 0x7ff0000000000001, 0x7fefffffffffffff
		}
		for _, test := range []struct {
			a, b  uint64
			truth [6]bool
		}{
			{0, sign, [6]bool{true, false, false, false, true, true}},
			{1, 0, [6]bool{false, true, false, true, false, true}},
			{sign | 1, 0, [6]bool{false, true, true, false, true, false}},
			{one, one, [6]bool{true, false, false, false, true, true}},
			{one | sign, one, [6]bool{false, true, true, false, true, false}},
			{inf, max, [6]bool{false, true, false, true, false, true}},
			{inf | sign, max | sign, [6]bool{false, true, true, false, true, false}},
			{nan, one, [6]bool{false, true, false, false, false, false}},
			{one, snan, [6]bool{false, true, false, false, false, false}},
			{snan, nan | sign, [6]bool{false, true, false, false, false, false}},
		} {
			for relation, want := range test.truth {
				if got := swarFloatComparisonOracle(test.a, test.b, width, relation); got != want {
					t.Fatal(width, test, relation, got, want)
				}
			}
		}
	}
}

func TestSWARFloatComparisonAdmissionAndMapping(t *testing.T) {
	for kind := uint32(65); kind <= 76; kind++ {
		width, relation, ok := swarFloatComparisonOpcode(kind)
		base, wantWidth := uint32(65), uint8(32)
		if kind >= 71 {
			base, wantWidth = 71, 64
		}
		if !ok || width != wantWidth || relation != swarFloatRelation(kind-base) {
			t.Fatal("comparison mapping", kind, width, relation, ok)
		}
		fn, ft := swarValidatedFunction(t, swarArithmeticFixture(kind))
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); len(code) == 0 || err != nil {
			t.Fatal("valid vector float comparison", kind, err)
		}
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{}); code != nil || err == nil {
			t.Fatal("FP admitted through isolated integer bridge", kind, err)
		}
		badType := *ft
		badType.Results = []wasm.ValType{wasm.I32}
		if code, _, err := compileSWARPairs(fn, &badType, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("comparison result class", kind, err)
		}
		for _, prefix := range [][]byte{nil, {0x20, 0}, {0x20, 0, 0x43, 0, 0, 0, 0}, {0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x20, 1}} {
			bad := *fn
			bad.BodyBytes = append(append([]byte(nil), prefix...), fn.BodyBytes[4:]...)
			if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("float comparison class/underflow", kind, err)
			}
		}
	}
	for _, kind := range []uint32{0, 64, 77, 214, ^uint32(0)} {
		if _, _, ok := swarFloatComparisonOpcode(kind); ok {
			t.Fatal("floating comparison neighbor mapped", kind)
		}
	}
}

func TestSWARFloatComparisonHelpersRejectBeforeBytes(t *testing.T) {
	for _, test := range []struct {
		width    uint8
		relation swarFloatRelation
	}{{0, swarFloatEQ}, {8, swarFloatEQ}, {16, swarFloatEQ}, {63, swarFloatLT}, {65, swarFloatGE}, {255, swarFloatEQ}, {32, 6}, {64, 255}} {
		f := integerEmitter{}
		f.a.Addi(enc.X5, enc.Zero, 0)
		before := len(f.a.B)
		f.swarFloatCompare(test.width, test.relation)
		if f.err == nil || len(f.a.B) != before {
			t.Fatal("invalid float helper emitted", test, f.err)
		}
		f.swarFloatCompare(32, swarFloatEQ)
		if len(f.a.B) != before {
			t.Fatal("sticky float compare error appended")
		}
	}
}
