//go:build amd64

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"math"
	"testing"
)

func TestFloatParitySingleExtension(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, op := range []wOp{opEq, opNe} {
			for _, dst := range []Reg{RAX, RDI, R8} {
				for _, tmp := range []Reg{RCX, RSI, R9} {
					f := fn{a: &encoder.Asm{}, reserved: ^maskOf(dst, tmp)}
					f.emitFCmpSetcc(op, 8, 9, wide, dst)
					first, parity, byteOp := condE, condNP, byte(0x20)
					if op == opNe {
						first, parity, byteOp = condNE, condP, 0x08
					}
					var want, old encoder.Asm
					want.Ucomis(8, 9, wide)
					want.SetccReg8(first, dst)
					want.SetccReg8(parity, tmp)
					want.AluRR8(byteOp, dst, tmp)
					want.Movzx8(dst, dst, false)
					old.Ucomis(8, 9, wide)
					old.SetccReg(first, dst)
					old.SetccReg(parity, tmp)
					old.AluRR(aluTable[opAnd].rr, dst, tmp, false)
					if op == opNe {
						old = encoder.Asm{}
						old.Ucomis(8, 9, wide)
						old.SetccReg(first, dst)
						old.SetccReg(parity, tmp)
						old.AluRR(aluTable[opOr].rr, dst, tmp, false)
					}
					if !bytes.Equal(f.a.B, want.B) || len(f.a.B) >= len(old.B) {
						t.Fatalf("wide=%v op=%v dst=%v tmp=%v got=%x want=%x old-bytes=%d", wide, op, dst, tmp, f.a.B, want.B, len(old.B))
					}
				}
			}
		}
	}
}

func TestFloatBooleanMaterializationMeasurement(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, op := range []wOp{opGtS, opGeS, opLtS, opLeS} {
			for _, dst := range []Reg{RAX, R8} {
				f := fn{a: &encoder.Asm{}}
				f.emitFCmpSetcc(op, 8, 9, wide, dst)
				t.Logf("wide=%v op=%v dst=%v native-bytes=%d code=%x", wide, op, dst, len(f.a.B), f.a.B)
			}
		}
	}
}

func TestFloatBooleanPressureAndNaN(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for k := byte(0); k < 6; k++ {
			typ, reinterpret, compare := wasm.I32, byte(0xbe), byte(0x5b)
			if wide {
				typ, reinterpret, compare = wasm.I64, 0xbf, 0x61
			}
			body := []byte{1, 24, 0x7f}
			for i := 0; i < 24; i++ {
				body = append(body, 0x20, 2, 0x41, byte(i+1), 0x6a, 0x22, byte(i+3))
			}
			body = append(body, 0x20, 0, reinterpret, 0x20, 1, reinterpret, compare+k)
			for i := 0; i < 24; i++ {
				body = append(body, 0x73)
			}
			body = append(body, 0x0b)
			m := mod1(t, []wasm.ValType{typ, typ, wasm.I32}, []wasm.ValType{wasm.I32}, body)
			cm, e := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true})
			if e != nil {
				t.Fatal(e)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			vals := []uint64{0, 0x80000000, 0x7f800000, 0x7fc12345, 0x7f812345, 0xffc54321, 0x3f800000}
			if wide {
				vals = []uint64{0, 0x8000000000000000, 0x7ff0000000000000, 0x7ff8000000001234, 0x7ff0000000001234, 0xfff8000000004321, 0x3ff0000000000000}
			}
			for _, a := range vals {
				for _, b := range vals {
					x, y := math.Float64frombits(a), math.Float64frombits(b)
					if !wide {
						x, y = float64(math.Float32frombits(uint32(a))), float64(math.Float32frombits(uint32(b)))
					}
					pred := false
					switch k {
					case 0:
						pred = x == y
					case 1:
						pred = x != y
					case 2:
						pred = x < y
					case 3:
						pred = x > y
					case 4:
						pred = x <= y
					case 5:
						pred = x >= y
					}
					want := uint32(0)
					if pred {
						want = 1
					}
					for i := 0; i < 24; i++ {
						want ^= 7 + uint32(i+1)
					}
					got := uint32(runCompiledAmd64u(t, cm, a, b, 7))
					if got != want {
						t.Fatalf("wide=%v op=%d a=%x b=%x got=%x want=%x", wide, k, a, b, got, want)
					}
				}
			}
		}
	}
}
