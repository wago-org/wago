//go:build linux && !tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

var swarProductKinds = []uint32{118, 119, 120, 121, 149, 150, 151, 152, 153, 181, 182, 183, 184, 185, 213}

func TestSWARProductAdmissionAndExactClasses(t *testing.T) {
	for _, kind := range swarProductKinds {
		fn, ft := swarValidatedFunction(t, swarArithmeticFixture(kind))
		if code, _, err := compileSWARPairs(fn, ft, emissionMode{production: true}); len(code) == 0 || err != nil {
			t.Fatal("valid multiply/minmax", kind, err)
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

func TestSWARProductOpcodeTable(t *testing.T) {
	for _, group := range []struct {
		base  uint32
		width uint8
	}{{118, 8}, {150, 16}, {182, 32}} {
		for offset, operation := range []swarProduct{swarMinS, swarMinU, swarMaxS, swarMaxU} {
			width, got, ok := swarProductOpcode(group.base + uint32(offset))
			if !ok || width != group.width || got != operation {
				t.Fatal(group, offset, width, got, ok)
			}
		}
	}
	for i, kind := range []uint32{149, 181, 213} {
		width, got, ok := swarProductOpcode(kind)
		if !ok || width != uint8(16<<i) || got != swarMul {
			t.Fatal(kind, width, got, ok)
		}
	}
	for _, kind := range []uint32{0, 117, 122, 148, 154, 180, 186, 212, 214, ^uint32(0)} {
		if _, _, ok := swarProductOpcode(kind); ok {
			t.Fatal("neighbor admitted", kind)
		}
	}
}

func TestSWARProductHelpersRejectBeforeBytes(t *testing.T) {
	for _, test := range []struct {
		width     uint8
		operation swarProduct
	}{{0, swarMul}, {24, swarMinS}, {65, swarMul}, {8, swarMul}, {64, swarMinS}, {64, swarMaxU}, {16, 5}, {32, 255}} {
		f := integerEmitter{}
		f.a.Addi(enc.X5, enc.Zero, 1)
		before := len(f.a.B)
		f.swarProduct(test.width, test.operation)
		if f.err == nil || len(f.a.B) != before {
			t.Fatal("invalid helper partially emitted", test, f.err)
		}
		f.swarProduct(16, swarMul)
		if len(f.a.B) != before {
			t.Fatal("sticky product error appended bytes")
		}
	}
}
