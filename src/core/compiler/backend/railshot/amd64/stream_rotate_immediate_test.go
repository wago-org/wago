//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"bytes"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"math/bits"
	"testing"
)

func TestStreamImmediateRotateEncoding(t *testing.T) {
	for _, op := range []byte{0x77, 0x78, 0x89, 0x8a} {
		wide, _ := streamRotateShape(op)
		for _, count := range []int64{0, 1, 7, 31, 32, 63, 64, 65, -1, -2147483648, -9223372036854775808} {
			sc := newScratch()
			f := fn{a: sc.asm, sc: sc}
			s := streamState{target: &f, regs: []uint8{uint8(R8), uint8(R9), uint8(R10)}, reserved: uint64(1) << RCX}
			s.add(streamNode{})
			value := s.add(streamNode{kind: shared.ScalarRegister, reg: uint8(R8), refs: 1, wide: wide, constant: 1})
			countID := s.add(streamNode{kind: shared.ScalarConstant, constant: count, refs: 1, wide: wide})
			rcx := s.add(streamNode{kind: shared.ScalarRegister, reg: uint8(RCX), refs: 1, wide: true})
			s.owners[R8], s.owners[RCX] = value, rcx
			s.stack = []streamID{value, countID}
			if err := s.integerRotate(op); err != nil {
				t.Fatal(err)
			}
			mask := uint64(31)
			if wide {
				mask = 63
			}
			amount := byte(uint64(count) & mask)
			var want encoder.Asm
			if !wide && amount == 0 {
				want.MovRegReg32(R8, R8)
			}
			digit := byte(0)
			if op == 0x78 || op == 0x8a {
				digit = 1
			}
			want.ShiftImm(digit, R8, amount, wide)
			if !bytes.Equal(f.a.B, want.B) {
				t.Fatalf("op=%x count=%d native=%x want=%x", op, count, f.a.B, want.B)
			}
			if s.owners[RCX] != rcx || s.node(rcx).kind != shared.ScalarRegister || s.Spills != 0 {
				t.Fatal("immediate rotate disturbed leased RCX")
			}
			if len(s.stack) != 1 || s.node(s.stack[0]).reg != uint8(R8) {
				t.Fatal("incorrect immediate result owner")
			}
		}
	}
}

func TestStreamImmediateRotateRuntime(t *testing.T) {
	streamMemoryTestEnable(t)
	for _, op := range []byte{0x77, 0x78, 0x89, 0x8a} {
		wide, _ := streamRotateShape(op)
		typ := streamNumericType(wide)
		for _, count := range []uint64{0, 1, 7, 31, 32, 63, 64, 65, ^uint64(0), 0x80000000, 1 << 63} {
			t.Run(fmt.Sprintf("%x/%x", op, count), func(t *testing.T) {
				body := append([]byte{0, 0x20, 0}, streamConstBytes(wide, count)...)
				xor := byte(0x73)
				if wide {
					xor = 0x85
				}
				body = append(body, op, 0x20, 0, xor, 0x0b) // Source alias must survive.
				m := mod1(t, []wasm.ValType{typ}, []wasm.ValType{typ}, body)
				streamNumericAdmitted(t, m)
				if diagnosticsEnabled && !regallocCheckEnabled {
					if n := compileWithStats(t, m, false).Funcs[0].Peephole["streaming-rotate-immediate"]; n != 1 {
						t.Fatalf("immediate selector=%d", n)
					}
				}
				cm, err := CompileModule(m)
				if err != nil {
					t.Fatal(err)
				}
				if cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				for _, x := range []uint64{0, 1, 0xdeadbeef87654321, ^uint64(0)} {
					n := int(count & 63)
					if op == 0x78 || op == 0x8a {
						n = -n
					}
					want := bits.RotateLeft64(x, n) ^ x
					if !wide {
						want = uint64(bits.RotateLeft32(uint32(x), n) ^ uint32(x))
					}
					if got := runCompiledAmd64u(t, cm, x); got != want {
						t.Fatalf("x=%x got=%x want=%x", x, got, want)
					}
				}
			})
		}
		// The constant tag is shared by both operand roots and a retained local.
		body := append([]byte{1, 1, 0x7f}, streamConstBytes(wide, 7)...)
		if wide {
			body[2] = 0x7e
		}
		body = append(body, 0x22, 0, 0x20, 0, op, 0x0b)
		m := mod1(t, nil, []wasm.ValType{typ}, body)
		streamNumericAdmitted(t, m)
		n := 7
		if op == 0x78 || op == 0x8a {
			n = -7
		}
		want := bits.RotateLeft64(7, n)
		if !wide {
			want = uint64(bits.RotateLeft32(7, n))
		}
		if got := runAmd64u(t, m); got != want {
			t.Fatalf("constant alias got=%x want=%x", got, want)
		}
	}
}
