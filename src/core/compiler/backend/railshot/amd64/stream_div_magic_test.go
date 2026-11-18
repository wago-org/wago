//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"bytes"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func TestStreamMagic32Encoding(t *testing.T) {
	old := streamingMagic32Enabled
	streamingMagic32Enabled = true
	t.Cleanup(func() { streamingMagic32Enabled = old })
	for _, d := range []uint32{3, 7, 97, 0x80000001, 0xffffffff} {
		for _, op := range []byte{0x6e, 0x70} {
			sc := newScratch()
			f := fn{a: sc.asm, sc: sc}
			s := streamState{target: &f, regs: []uint8{uint8(R8), uint8(R9), uint8(R10)}}
			s.add(streamNode{})
			left := s.add(streamNode{kind: shared.ScalarRegister, reg: uint8(R8), refs: 2})
			right := s.add(streamNode{kind: shared.ScalarConstant, constant: int64(int32(d)), refs: 1})
			s.owners[R8] = left
			s.stack = []streamID{left, right}
			if !s.integerDivMagic32(op) {
				t.Fatal("not handled")
			}
			magic, shift, add := railshot.MagicU(uint64(d), 32)
			var want encoder.Asm
			if add {
				want.MovRegReg32(RAX, R8)
				want.MovImm64(R9, ^uint64(0)/uint64(d)+1)
				want.Mul(R9, true)
				want.MovRegReg32(R9, RDX)
			} else {
				want.MovRegReg32(R9, R8)
				want.MovImm32(R10, int32(uint32(magic)))
				want.IMul(R9, R10, true)
				want.ShiftImm(5, R9, 32+byte(shift), true)
			}
			if op == 0x70 {
				want.ImulRI(R9, int32(d), false)
				want.Neg(R9, false)
				want.AluRR(0x01, R9, R8, false)
			}
			if !bytes.Equal(f.a.B, want.B) {
				t.Fatalf("d=%d op=%x native=%x want=%x", d, op, f.a.B, want.B)
			}
			if s.node(left).refs != 1 || s.owners[R8] != left {
				t.Fatal("retained source disturbed")
			}
		}
	}
}

func TestStreamMagic32Differential(t *testing.T) {
	old := streamingMagic32Enabled
	streamingMagic32Enabled = true
	t.Cleanup(func() { streamingMagic32Enabled = old })
	streamMemoryTestEnable(t)
	divisors := []uint32{3, 5, 7, 9, 10, 13, 31, 33, 63, 65, 97, 127, 129, 255, 257, 65535, 65537, 0x7fffffff, 0x80000001, 0xfffffffb, 0xffffffff}
	seed := uint32(0x13579bdf)
	for range 16 {
		seed = seed*1664525 + 1013904223
		divisors = append(divisors, seed|3)
	}
	for _, d := range divisors {
		for _, op := range []byte{0x6e, 0x70} {
			t.Run(fmt.Sprintf("%x/%x", op, d), func(t *testing.T) {
				body := append([]byte{0, 0x20, 0}, streamConstBytes(false, uint64(d))...)
				body = append(body, op, 0x20, 0, 0x73, 0x0b)
				m := mod1(t, []wasm.ValType{streamNumericType(false)}, []wasm.ValType{streamNumericType(false)}, body)
				streamNumericAdmitted(t, m)
				if diagnosticsEnabled && !regallocCheckEnabled {
					_, _, add := railshot.MagicU(uint64(d), 32)
					counter := "streaming-magic-product-shift"
					if add {
						counter = "streaming-magic-wide-u32"
					}
					if compileWithStats(t, m, false).Funcs[0].Peephole[counter] != 1 {
						t.Fatal("missing positive path counter")
					}
				}
				cm, err := CompileModule(m)
				if err != nil {
					t.Fatal(err)
				}
				if cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				xs := []uint32{0, 1, d - 1, d, d + 1, 0x7fffffff, 0x80000000, 0xfffffffe, 0xffffffff}
				for _, k := range []uint64{2, 3, 7, 31, 65535} {
					p := k * uint64(d)
					if p <= 0xffffffff {
						xs = append(xs, uint32(p)-1, uint32(p), uint32(p)+1)
					}
				}
				for range 256 {
					seed = seed*1664525 + 1013904223
					xs = append(xs, seed)
				}
				for _, x := range xs {
					q := x / d
					if op == 0x70 {
						q = x % d
					}
					want := uint64(q ^ x)
					got := runCompiledAmd64u(t, cm, uint64(x)|0xdeadbeef00000000)
					if got != want {
						t.Fatalf("x=%x got=%x want=%x", x, got, want)
					}
				}
			})
		}
	}
}
