//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

var swarSaturationKinds = []uint32{111, 112, 114, 115, 123, 143, 144, 146, 147, 155}

func TestSWARSaturationAdmissionAndExactClasses(t *testing.T) {
	for _, kind := range swarSaturationKinds {
		fn, ft := swarValidatedFunction(t, swarArithmeticFixture(kind))
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); len(code) == 0 || err != nil {
			t.Fatal("valid saturation/average", kind, err)
		}
		badType := *ft
		badType.Results = []wasm.ValType{wasm.I64}
		if code, _, err := compileSWARPairs(fn, &badType, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("wrong result class emitted", kind, err)
		}
		for _, prefix := range [][]byte{nil, {0x20, 0}, {0x20, 0, 0x42, 0}, {0x41, 0, 0x20, 1}} {
			bad := *fn
			bad.BodyBytes = append(append([]byte(nil), prefix...), fn.BodyBytes[4:]...)
			if code, _, err := compileSWARPairs(&bad, ft, emissionMode{production: true}); code != nil || err == nil {
				t.Fatal("binary operand underflow/class emitted", kind, err)
			}
		}
	}
	for _, kind := range []uint32{101, 102, 133, 134, 232, 233, 244, 245} {
		fn, ft := swarValidatedFunction(t, swarArithmeticFixture(kind))
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); code != nil || err == nil {
			t.Fatal("pending narrowing/FP minmax emitted", kind, err)
		}
	}
}

func TestSWARSaturationOpcodeTable(t *testing.T) {
	for _, group := range []struct {
		base  uint32
		width uint8
	}{{111, 8}, {143, 16}} {
		for _, test := range []struct {
			offset    uint32
			operation swarSaturation
		}{{0, swarAddSatS}, {1, swarAddSatU}, {3, swarSubSatS}, {4, swarSubSatU}, {12, swarAvgrU}} {
			width, got, ok := swarSaturationOpcode(group.base + test.offset)
			if !ok || width != group.width || got != test.operation {
				t.Fatal(group, test, width, got, ok)
			}
		}
	}
	for _, kind := range []uint32{0, 110, 113, 116, 122, 124, 142, 145, 148, 154, 156, ^uint32(0)} {
		if _, _, ok := swarSaturationOpcode(kind); ok {
			t.Fatal("neighbor admitted", kind)
		}
	}
}

func TestSWARSaturationHelpersRejectBeforeBytes(t *testing.T) {
	for _, test := range []struct {
		width     uint8
		operation swarSaturation
	}{{0, swarAddSatS}, {24, swarSubSatS}, {65, swarAddSatS}, {32, swarAddSatS}, {64, swarSubSatS}, {64, swarAvgrU}, {16, 5}, {8, 255}} {
		f := integerEmitter{}
		f.a.Addi(enc.X5, enc.Zero, 1)
		before := len(f.a.B)
		f.swarSaturate(test.width, test.operation)
		if f.err == nil || len(f.a.B) != before {
			t.Fatal("invalid helper partially emitted", test, f.err)
		}
		f.swarSaturate(16, swarAddSatS)
		if len(f.a.B) != before {
			t.Fatal("sticky saturation error appended bytes")
		}
	}
}
