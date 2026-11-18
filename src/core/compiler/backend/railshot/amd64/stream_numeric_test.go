//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"golang.org/x/sys/cpu"
	"math/bits"
	"os"
	"strings"
	"testing"
)

func streamNumericType(w bool) wasm.ValType {
	if w {
		return wasm.I64
	}
	return wasm.I32
}
func streamNumericExpected(op byte, x uint64) uint64 {
	switch op {
	case 0x45:
		if uint32(x) == 0 {
			return 1
		}
		return 0
	case 0x50:
		if x == 0 {
			return 1
		}
		return 0
	case 0x67:
		return uint64(bits.LeadingZeros32(uint32(x)))
	case 0x68:
		return uint64(bits.TrailingZeros32(uint32(x)))
	case 0x69:
		return uint64(bits.OnesCount32(uint32(x)))
	case 0x79:
		return uint64(bits.LeadingZeros64(x))
	case 0x7a:
		return uint64(bits.TrailingZeros64(x))
	case 0x7b:
		return uint64(bits.OnesCount64(x))
	case 0xa7, 0xad:
		return uint64(uint32(x))
	case 0xac, 0xc4:
		return uint64(int64(int32(x)))
	case 0xc0:
		return uint64(uint32(int32(int8(x))))
	case 0xc1:
		return uint64(uint32(int32(int16(x))))
	case 0xc2:
		return uint64(int64(int8(x)))
	case 0xc3:
		return uint64(int64(int16(x)))
	}
	panic("unexpected opcode")
}

func streamNumericAdmitted(t *testing.T, m *wasm.Module) {
	t.Helper()
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	if !admitStreamBody(&m.Code[0], &m.Types[0].SubTypes[0].Comp).Eligible {
		t.Fatal("numeric fixture not admitted")
	}
	if diagnosticsEnabled && !regallocCheckEnabled {
		if n := compileWithStats(t, m, false).Funcs[0].Peephole["streaming-function"]; n != 1 {
			t.Fatalf("streaming-function=%d", n)
		}
	}
}

func TestStreamNumericUnary(t *testing.T) {
	streamMemoryTestEnable(t)
	for _, op := range []byte{0x45, 0x50, 0x67, 0x68, 0x69, 0x79, 0x7a, 0x7b, 0xa7, 0xac, 0xad, 0xc0, 0xc1, 0xc2, 0xc3, 0xc4} {
		t.Run(fmt.Sprintf("%x", op), func(t *testing.T) {
			iw, rw, _ := streamUnaryShape(op)
			m := mod1(t, []wasm.ValType{streamNumericType(iw)}, []wasm.ValType{streamNumericType(rw)}, []byte{0, 0x20, 0, op, 0x0b})
			streamNumericAdmitted(t, m)
			var hardware uint8
			if cpu.X86.HasBMI1 {
				hardware |= shared.BitCountTZCNT
			}
			if cpu.X86.HasPOPCNT {
				hardware |= shared.BitCountPOPCNT
			}
			// x/sys does not expose LZCNT. Linux names its CPUID bit "abm".
			if info, err := os.ReadFile("/proc/cpuinfo"); err == nil {
				for _, field := range strings.Fields(string(info)) {
					if field == "abm" {
						hardware |= shared.BitCountLZCNT
						break
					}
				}
			}
			for _, features := range []uint8{0, hardware} {
				cm, err := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: shared.AMD64BitCountRequirements(features), BitCountFeatures: features})
				if err != nil {
					t.Fatal(err)
				}
				if cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				for _, x := range []uint64{0, 1, 0x80, 0x8000, 0x80000000, 0xffffffff, 0x8000000000000000, 0xffffffffffffffff, 0x123456789abcdef0} {
					if got, want := runCompiledAmd64u(t, cm, x), streamNumericExpected(op, x); got != want {
						t.Fatalf("features=%x x=%x got=%x want=%x", features, x, got, want)
					}
				}
			}
			// The source local remains live across the operation. A shared register
			// must not be overwritten by the unary result.
			if iw == rw {
				add := byte(0x6a)
				if rw {
					add = 0x7c
				}
				m = mod1(t, []wasm.ValType{streamNumericType(iw)}, []wasm.ValType{streamNumericType(rw)}, []byte{0, 0x20, 0, op, 0x20, 0, add, 0x0b})
				streamNumericAdmitted(t, m)
				x := uint64(0xfedcba9876543280)
				want := streamNumericExpected(op, x) + x
				if !rw {
					want = uint64(uint32(want))
				}
				if got := runAmd64u(t, m, x); got != want {
					t.Fatalf("shared input got=%x want=%x", got, want)
				}
			}
		})
	}
}

func TestStreamNumericSelect(t *testing.T) {
	streamMemoryTestEnable(t)
	for _, wide := range []bool{false, true} {
		typ := streamNumericType(wide)
		m := mod1(t, []wasm.ValType{typ, typ, wasm.I32}, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x20, 1, 0x20, 2, 0x1b, 0x0b})
		streamNumericAdmitted(t, m)
		for _, cond := range []uint64{0, 1, 0xffffffff, 0x100000000} {
			left, right := uint64(0x123456789abcdef0), uint64(0xfedcba9876543210)
			want := right
			if uint32(cond) != 0 {
				want = left
			}
			if !wide {
				want = uint64(uint32(want))
			}
			if got := runAmd64u(t, m, left, right, cond); got != want {
				t.Fatalf("wide=%v cond=%x got=%x want=%x", wide, cond, got, want)
			}
		}
	}
	// All inputs alias, including the condition. Release each root exactly once.
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x20, 0, 0x20, 0, 0x1b, 0x20, 0, 0x6a, 0x0b})
	streamNumericAdmitted(t, m)
	if got := runAmd64u(t, m, 7); got != 14 {
		t.Fatalf("aliased select=%d", got)
	}
	// The condition is created before a deferred variable shift. Materializing
	// the shift can clobber RCX, so selection must reload its current location.
	m = mod1(t, []wasm.ValType{wasm.I32, wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, []byte{1, 1, 0x7f, 0x20, 2, 0x41, 1, 0x6a, 0x21, 3, 0x20, 0, 0x20, 1, 0x74, 0x41, 9, 0x20, 3, 0x1b, 0x0b})
	streamNumericAdmitted(t, m)
	for _, c := range []uint64{0, 0xffffffff} {
		want := uint64(24)
		if c == 0xffffffff {
			want = 9
		}
		if got := runAmd64u(t, m, 3, 3, c); got != want {
			t.Fatalf("deferred select got%d want%d", got, want)
		}
	}
}

func TestStreamNumericRotate(t *testing.T) {
	streamMemoryTestEnable(t)
	for _, op := range []byte{0x77, 0x78, 0x89, 0x8a} {
		wide, _ := streamRotateShape(op)
		typ := streamNumericType(wide)
		m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x20, 1, op, 0x0b})
		streamNumericAdmitted(t, m)
		for _, count := range []uint64{0, 1, 31, 32, 63, 64, 65, 0xffffffffffffffff} {
			x := uint64(0x1234567887654321)
			n := int(count & 63)
			if op == 0x78 || op == 0x8a {
				n = -n
			}
			want := bits.RotateLeft64(x, n)
			if !wide {
				want = uint64(bits.RotateLeft32(uint32(x), n))
			}
			if got := runAmd64u(t, m, x, count); got != want {
				t.Fatalf("op=%x count=%x got=%x want=%x", op, count, got, want)
			}
		}
		m = mod1(t, []wasm.ValType{typ}, []wasm.ValType{typ}, []byte{0, 0x20, 0, 0x20, 0, op, 0x0b})
		streamNumericAdmitted(t, m)
		n := 3
		if op == 0x78 || op == 0x8a {
			n = -3
		}
		want := bits.RotateLeft64(3, n)
		if !wide {
			want = uint64(bits.RotateLeft32(3, n))
		}
		if got := runAmd64u(t, m, 3); got != want {
			t.Fatalf("aliased rotate=%x want%x", got, want)
		}
	}
}
