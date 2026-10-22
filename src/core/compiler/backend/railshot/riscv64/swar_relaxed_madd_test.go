//go:build linux && !tinygo

package riscv64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func TestSWARRelaxedMaddAdmissionAndUnfusedStaticRNE(t *testing.T) {
	for kind := uint32(261); kind <= 264; kind++ {
		width, negative, ok := swarRelaxedMaddOpcode(kind)
		wantWidth := uint8(32)
		if kind >= 263 {
			wantWidth = 64
		}
		if !ok || width != wantWidth || negative != (kind == 262 || kind == 264) {
			t.Fatal("relaxed madd mapping", kind, width, negative, ok)
		}
		fn, ft := swarValidatedFunction(t, swarRelaxedFixture(kind))
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); len(code) == 0 || err != nil {
			t.Fatal("relaxed madd", kind, err)
		}
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{}); code != nil || err == nil {
			t.Fatal("madd through isolated bridge", kind, err)
		}
		badType := *ft
		badType.Results = []wasm.ValType{wasm.F64}
		if code, _, err := compileSWARPairs(fn, &badType, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("madd result class", kind, err)
		}
		for _, prefix := range [][]byte{nil, {0x20, 0}, {0x20, 0, 0x20, 1}, {0x41, 0, 0x20, 1, 0x20, 2}, {0x20, 0, 0x41, 0, 0x20, 2}, {0x20, 0, 0x20, 1, 0x41, 0}} {
			bad := *fn
			bad.BodyBytes = append(append([]byte(nil), prefix...), fn.BodyBytes[6:]...)
			if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("madd input class/underflow", kind, err)
			}
		}
		f := integerEmitter{}
		f.swarRelaxedMadd(width, negative)
		adds, muls := 0, 0
		for offset := 0; offset < len(f.a.B); offset += 4 {
			word := binary.LittleEndian.Uint32(f.a.B[offset:])
			switch word & 0x7f {
			case 0x43, 0x47, 0x4b, 0x4f:
				t.Fatal("fused relaxed operation", kind, word)
			case 0x53:
				funct7 := (word >> 25) & 0x7f
				if funct7 == 0 || funct7 == 1 {
					adds++
				} else if funct7 == 8 || funct7 == 9 {
					muls++
				} else {
					continue
				}
				if (word>>12)&7 != uint32(enc.RNE) {
					t.Fatal("nonstaticRNE madd", kind, word)
				}
			}
		}
		if f.err != nil || f.a.Err() != nil || adds != 128/int(width) || muls != adds {
			t.Fatal("unfused lane operations", kind, adds, muls, f.err, f.a.Err())
		}
	}
	for _, kind := range []uint32{0, 260, 265, ^uint32(0)} {
		if _, _, ok := swarRelaxedMaddOpcode(kind); ok {
			t.Fatal("madd neighbor", kind)
		}
	}
	for _, width := range []uint8{0, 16, 63, 65, 255} {
		f := integerEmitter{}
		f.a.Addi(enc.X5, enc.Zero, 0)
		before := len(f.a.B)
		f.swarRelaxedMadd(width, false)
		if f.err == nil || len(f.a.B) != before {
			t.Fatal("invalid madd emitted", width)
		}
		f.swarRelaxedMadd(32, true)
		if len(f.a.B) != before {
			t.Fatal("sticky madd error appended")
		}
	}
}
