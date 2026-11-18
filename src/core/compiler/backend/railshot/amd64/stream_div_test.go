//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	wRuntime "github.com/wago-org/wago/src/core/runtime"
)

func streamDivExpected(a, b uint64, wide, signed, rem bool) uint64 {
	if !wide {
		a, b = uint64(uint32(a)), uint64(uint32(b))
	}
	var result uint64
	if signed {
		x, y := int64(a), int64(b)
		if !wide {
			x, y = int64(int32(a)), int64(int32(b))
		}
		if rem {
			result = uint64(x % y)
		} else {
			result = uint64(x / y)
		}
	} else if rem {
		result = a % b
	} else {
		result = a / b
	}
	if !wide {
		result = uint64(uint32(result))
	}
	return result
}

func TestStreamDivisionRuntime(t *testing.T) {
	streamMemoryTestEnable(t)
	for _, op := range []byte{0x6d, 0x6e, 0x6f, 0x70, 0x7f, 0x80, 0x81, 0x82} {
		t.Run(fmt.Sprintf("%x", op), func(t *testing.T) {
			wide, signed, rem, _ := streamDivShape(op)
			typ := wasm.I32
			xor := byte(0x73)
			if wide {
				typ = wasm.I64
				xor = 0x85
			}
			// Keep both argument aliases live after division's fixed-register clobbers.
			body := []byte{0, 0x20, 0, 0x20, 1, op, 0x20, 0, xor, 0x20, 1, xor, 0xb}
			m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, body)
			streamMemoryTestAdmitted(t, m)
			cm, err := CompileModule(m)
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			pairs := [][2]uint64{{0, 1}, {1, 1}, {7, 2}, {^uint64(0), 1}, {^uint64(0), 2}, {1 << 63, 2}, {0x7fffffff, 3}, {123456789, 97}, {0xdeadbeef00000007, 2}, {u64(-7), 3}, {u64(-7), u64(-3)}, {1 << 31, 2}}
			for _, pair := range pairs {
				a, b := pair[0], pair[1]
				want := streamDivExpected(a, b, wide, signed, rem) ^ a ^ b
				if !wide {
					want = uint64(uint32(want))
				}
				if got := runCompiledAmd64u(t, cm, a, b); got != want {
					t.Fatalf("a=%x b=%x got=%x want=%x", a, b, got, want)
				}
			}
			// Same logical ID in both operand positions must survive two releases.
			alias := mod1(t, []wasm.ValType{typ}, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x20, 0, op, 0xb})
			streamMemoryTestAdmitted(t, alias)
			for _, value := range []uint64{1, 17, ^uint64(0)} {
				want := uint64(1)
				if rem {
					want = 0
				}
				if got := runAmd64u(t, alias, value); got != want {
					t.Fatalf("same-ID value=%x got=%x want=%x", value, got, want)
				}
			}
		})
	}
}

func TestStreamDivisionTrapsAndRemainderOverflow(t *testing.T) {
	streamMemoryTestEnable(t)
	for _, op := range []byte{0x6d, 0x6e, 0x6f, 0x70, 0x7f, 0x80, 0x81, 0x82} {
		wide, signed, rem, _ := streamDivShape(op)
		typ := wasm.I32
		minimum := uint64(1 << 31)
		minusOne := uint64(0xffffffff)
		if wide {
			typ = wasm.I64
			minimum = 1 << 63
			minusOne = ^uint64(0)
		}
		// A dropped result must not suppress division's trap or move it after the
		// later unreachable. Distinct trap codes each retain an unambiguous PC.
		body := []byte{0, 0x20, 0, 0x20, 1, op, 0x1a, 0x00, 0xb}
		m := mod1(t, []wasm.ValType{typ, typ}, nil, body)
		streamMemoryTestAdmitted(t, m)
		cases := []struct {
			a, b uint64
			code wRuntime.TrapCode
			pc   uint32
		}{{7, 0, wRuntime.TrapDivZero, m.Code[0].LocalDeclBytes + 4}, {7, 2, wRuntime.TrapUnreachable, m.Code[0].LocalDeclBytes + 6}}
		if signed && !rem {
			cases = append(cases, struct {
				a, b uint64
				code wRuntime.TrapCode
				pc   uint32
			}{minimum, minusOne, wRuntime.TrapDivOverflow, m.Code[0].LocalDeclBytes + 4})
		}
		for _, interruptible := range []bool{false, true} {
			for _, tc := range cases {
				_, _, err := runMemAmd64WithOptions(t, m, CompileOptions{Interruptible: interruptible}, nil, tc.a, tc.b)
				var trap *wRuntime.TrapError
				if !errors.As(err, &trap) || trap.Code != tc.code {
					t.Fatalf("op=%x interruptible=%v a=%x b=%x wrongtrap %v", op, interruptible, tc.a, tc.b, err)
				}
				if len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 0 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != tc.pc {
					t.Fatalf("op=%x incorrect trap PC %+v want%d", op, trap.Frames, tc.pc)
				}
			}
		}
		if signed && rem {
			result := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x20, 1, op, 0xb})
			if got := runAmd64u(t, result, minimum, minusOne); got != 0 {
				t.Fatalf("op=%x INT_MIN remainder -1 got%x", op, got)
			}
		}
	}
}

func TestStreamDivisionNestedFixedResults(t *testing.T) {
	streamMemoryTestEnable(t)
	for _, wide := range []bool{false, true} {
		typ, div, rem, add := wasm.I32, byte(0x6e), byte(0x70), byte(0x6a)
		if wide {
			typ, div, rem, add = wasm.I64, 0x80, 0x82, 0x7c
		}
		m := mod1(t, []wasm.ValType{typ, typ, typ, typ}, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x20, 1, div, 0x20, 2, 0x20, 3, rem, add, 0xb})
		streamMemoryTestAdmitted(t, m)
		if got := runAmd64u(t, m, 100, 7, 90, 11); got != 16 {
			t.Fatalf("wide=%v got=%d want16", wide, got)
		}
	}
}
