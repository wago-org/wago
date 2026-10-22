//go:build linux && !tinygo

package riscv64

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"testing"
)

func floatFixture(wide bool, op byte, unary, integerResult bool) fixtureFunc {
	typ := byte(0x7d)
	if wide {
		typ = 0x7c
	}
	params := []byte{typ}
	body := []byte{0x20, 0}
	if !unary {
		params = append(params, typ)
		body = append(body, 0x20, 1)
	}
	result := typ
	if integerResult {
		result = 0x7f
	}
	return fixtureFunc{params: params, result: result, body: append(body, op, 0x0b)}
}

func TestProductionFloatStaticRoundingAndFingerprint(t *testing.T) {
	var functions []fixtureFunc
	for _, wide := range []bool{false, true} {
		base := byte(0x8b)
		if wide {
			base = 0x99
		}
		for n := byte(0); n < 14; n++ {
			functions = append(functions, floatFixture(wide, base+n, n <= 6, false))
		}
	}
	image, err := CompileProduction(binaryModule(functions...), ProductionOptions{DeferCodeMapping: true, Interruptible: true})
	if err != nil {
		t.Fatal(err)
	}
	defer image.CodeImage.Close()
	count := 0
	for pc := 0; pc < len(image.Code); pc += 4 {
		word := binary.LittleEndian.Uint32(image.Code[pc:])
		if word&127 != 0x53 {
			continue
		}
		f7 := word >> 25
		// Arithmetic/sqrt and conversions have rm fields. Sign/class/compare
		// operations interpret the same field as a separate operation selector.
		if f7 <= 0x0d || f7 >= 0x20 && f7 <= 0x2d || f7 >= 0x60 && f7 <= 0x69 {
			if mode := word >> 12 & 7; mode > 4 {
				t.Fatalf("nonstatic rounding mode %d at %d", mode, pc)
			}
			count++
		}
	}
	if count < 20 {
		t.Fatal("FP instructions missing", count)
	}
	t.Logf("FLOAT_CODE bytes=%d sha256=%x", len(image.Code), sha256.Sum256(image.Code))
}

func TestProductionFloatAdmissionAndIsolatedRejection(t *testing.T) {
	data := binaryModule(floatFixture(false, 0x92, false, false), floatFixture(true, 0xa0, false, false), fixtureFunc{result: 0x7c, unexported: true, body: []byte{0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x0b}})
	for _, deferred := range []bool{false, true} {
		image, err := CompileProduction(data, ProductionOptions{DeferCodeMapping: deferred})
		if err != nil || image == nil || len(image.Entry) != 3 {
			t.Fatal("FP module admission", image, err)
		}
		if err := image.CodeImage.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if c, err := Compile(data); c != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("isolated integer ABI admitted FP", c, err)
	}
	// Unsupported SIMD in an otherwise valid FP module still rejects as a whole.
	data = binaryModule(floatFixture(false, 0x92, false, false), fixtureFunc{unexported: true, body: []byte{0xfd, 0x0c, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x1a, 0x0b}})
	if image, err := CompileProduction(data, ProductionOptions{}); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("mixed unsupported shape published", image, err)
	}
}
