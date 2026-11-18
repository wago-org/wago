//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	wRuntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func streamConstBytes(wide bool, d uint64) []byte {
	if wide {
		return append([]byte{0x42}, wasmtest.SLEB64(int64(d))...)
	}
	return append([]byte{0x41}, wasmtest.SLEB32(int32(d))...)
}

func TestStreamConstantDivisionPowers(t *testing.T) {
	streamMemoryTestEnable(t)
	for _, op := range []byte{0x6d, 0x6e, 0x6f, 0x70, 0x7f, 0x80, 0x81, 0x82} {
		wide, signed, rem, _ := streamDivShape(op)
		typ := streamNumericType(wide)
		powers := []uint{0, 1, 2, 15, 30, 31}
		if wide {
			powers = append(powers, 32, 62, 63)
		}
		for _, k := range powers {
			ds := []uint64{uint64(1) << k}
			if signed {
				ds = append(ds, 0-ds[0])
			}
			for _, d := range ds {
				t.Run(fmt.Sprintf("%x/%x", op, d), func(t *testing.T) {
					xor := byte(0x73)
					if wide {
						xor = 0x85
					}
					body := append([]byte{0, 0x20, 0}, streamConstBytes(wide, d)...)
					// Retain the dividend local after lowering: input ownership must survive.
					body = append(body, op, 0x20, 0, xor, 0x0b)
					m := mod1(t, []wasm.ValType{typ}, []wasm.ValType{typ}, body)
					streamNumericAdmitted(t, m)
					if diagnosticsEnabled && !regallocCheckEnabled {
						if got := compileWithStats(t, m, false).Funcs[0].Peephole["streaming-div-pow2"]; got != 1 {
							t.Fatalf("power-of-two selection=%d", got)
						}
					}
					cm, err := CompileModule(m)
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					for _, x := range []uint64{0, 1, 7, u64(-7), 0x80000000, 0xffffffff, 1 << 63, ^uint64(0), 0xdeadbeef00000007, 0x123456789abcdef0} {
						min, minusOne := uint64(1<<63), ^uint64(0)
						xx, dd := x, d
						if !wide {
							min = 1 << 31
							minusOne = 0xffffffff
							xx = uint64(uint32(x))
							dd = uint64(uint32(d))
						}
						if signed && !rem && xx == min && dd == minusOne {
							continue
						}
						want := streamDivExpected(x, d, wide, signed, rem) ^ x
						if !wide {
							want = uint64(uint32(want))
						}
						if got := runCompiledAmd64u(t, cm, x); got != want {
							t.Fatalf("x=%x got=%x want=%x", x, got, want)
						}
					}
				})
			}
		}
	}
}

func TestStreamConstantDivisionDroppedOverflow(t *testing.T) {
	streamMemoryTestEnable(t)
	for _, wide := range []bool{false, true} {
		typ := streamNumericType(wide)
		op, minimum := byte(0x6d), uint64(1<<31)
		if wide {
			op = 0x7f
			minimum = 1 << 63
		}
		body := append([]byte{0, 0x20, 0}, streamConstBytes(wide, ^uint64(0))...)
		pc := uint32(len(body))
		body = append(body, op, 0x1a, 0x00, 0x0b)
		m := mod1(t, []wasm.ValType{typ}, nil, body)
		streamNumericAdmitted(t, m)
		for _, interruptible := range []bool{false, true} {
			_, _, err := runMemAmd64WithOptions(t, m, CompileOptions{Interruptible: interruptible}, nil, minimum)
			var trap *wRuntime.TrapError
			if !errors.As(err, &trap) || trap.Code != wRuntime.TrapDivOverflow {
				t.Fatalf("wide=%v trap=%v", wide, err)
			}
			if len(trap.Frames) != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != pc {
				t.Fatalf("frames=%+v want pc%d", trap.Frames, pc)
			}
		}
	}
}

func TestStreamConstantDivisionSameID(t *testing.T) {
	streamMemoryTestEnable(t)
	for _, wide := range []bool{false, true} {
		typ := streamNumericType(wide)
		op := byte(0x6e)
		if wide {
			op = 0x80
		}
		// local.tee preserves one constant ID for both operands and a surviving local.
		body := append([]byte{1, 1, byte(0x7f)}, streamConstBytes(wide, 8)...)
		if wide {
			body[2] = 0x7e
		}
		body = append(body, 0x22, 0, 0x20, 0, op, 0x0b)
		m := mod1(t, nil, []wasm.ValType{typ}, body)
		streamNumericAdmitted(t, m)
		if got := runAmd64u(t, m); got != 1 {
			t.Fatalf("alias result=%d", got)
		}
	}
}
