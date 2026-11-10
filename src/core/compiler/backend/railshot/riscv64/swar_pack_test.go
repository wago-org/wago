//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

var swarPackKinds = []uint32{101, 102, 133, 134, 156, 157, 158, 159, 188, 189, 190, 191, 220, 221, 222, 223}

func TestSWARPackAdmissionAndExactClasses(t *testing.T) {
	for _, kind := range swarPackKinds {
		fn, ft := swarValidatedFunction(t, swarArithmeticFixture(kind))
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); len(code) == 0 || err != nil {
			t.Fatal("valid narrow/extmultiply", kind, err)
		}
		badType := *ft
		badType.Results = []wasm.ValType{wasm.I64}
		if code, _, err := compileSWARPairs(fn, &badType, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("wrong pack result class", kind, err)
		}
		for _, prefix := range [][]byte{nil, {0x20, 0}, {0x20, 0, 0x42, 0}, {0x41, 0, 0x20, 1}} {
			bad := *fn
			bad.BodyBytes = append(append([]byte(nil), prefix...), fn.BodyBytes[4:]...)
			if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("pack class/underflow", kind, err)
			}
		}
	}
}

func TestSWARPackOpcodeTables(t *testing.T) {
	for _, group := range []struct {
		base  uint32
		width uint8
	}{{156, 8}, {188, 16}, {220, 32}} {
		for offset := uint32(0); offset < 4; offset++ {
			width, signed, high, ok := swarExtmultiplyOpcode(group.base + offset)
			if !ok || width != group.width || signed != (offset < 2) || high != (offset%2 == 1) {
				t.Fatal(group, offset, width, signed, high, ok)
			}
		}
	}
	for _, group := range []struct {
		base  uint32
		width uint8
	}{{101, 8}, {133, 16}} {
		for offset := uint32(0); offset < 2; offset++ {
			width, signed, ok := swarNarrowOpcode(group.base + offset)
			if !ok || width != group.width || signed != (offset == 0) {
				t.Fatal(group, offset, width, signed, ok)
			}
		}
	}
	for _, kind := range []uint32{0, 100, 103, 132, 135, 155, 160, 187, 192, 219, 224, ^uint32(0)} {
		if _, _, _, ok := swarExtmultiplyOpcode(kind); ok {
			t.Fatal("extmultiply neighbor admitted", kind)
		}
		if _, _, ok := swarNarrowOpcode(kind); ok {
			t.Fatal("narrow neighbor admitted", kind)
		}
	}
}

func TestSWARPackHelpersRejectBeforeBytes(t *testing.T) {
	for _, width := range []uint8{0, 1, 7, 24, 32, 64, 255} {
		for _, signed := range []bool{false, true} {
			f := integerEmitter{}
			f.a.Addi(enc.X5, enc.Zero, 1)
			before := len(f.a.B)
			f.swarNarrow(width, signed)
			if f.err == nil || len(f.a.B) != before {
				t.Fatal("invalid narrow partially emitted", width, signed, f.err)
			}
			f.swarNarrow(8, true)
			if len(f.a.B) != before {
				t.Fatal("sticky narrow error appended")
			}
			if width == 32 {
				continue
			}
			for _, high := range []bool{false, true} {
				f = integerEmitter{}
				f.a.Addi(enc.X5, enc.Zero, 1)
				before = len(f.a.B)
				f.swarExtmultiply(width, signed, high)
				if f.err == nil || len(f.a.B) != before {
					t.Fatal("invalid extmultiply partially emitted", width, signed, high, f.err)
				}
				f.swarExtmultiply(8, true, false)
				if len(f.a.B) != before {
					t.Fatal("sticky extmultiply error appended")
				}
			}
		}
	}
}
