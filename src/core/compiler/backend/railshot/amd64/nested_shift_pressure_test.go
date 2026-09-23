//go:build linux && amd64

package amd64

import (
	"fmt"
	"math/bits"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var shiftBoundaryCounts = [...]uint64{0, 31, 32, 63, 64, 127}

func shiftResult(width, op int, x, count uint64) uint64 {
	if width == 32 {
		x = uint64(uint32(x))
		count &= 31
		switch op {
		case 0:
			return uint64(uint32(x) << count)
		case 1:
			return uint64(uint32(int32(x) >> count))
		case 2:
			return x >> count
		case 3:
			return uint64(bits.RotateLeft32(uint32(x), int(count)))
		default:
			return uint64(bits.RotateLeft32(uint32(x), -int(count)))
		}
	}
	count &= 63
	switch op {
	case 0:
		return x << count
	case 1:
		return uint64(int64(x) >> count)
	case 2:
		return x >> count
	case 3:
		return bits.RotateLeft64(x, int(count))
	default:
		return bits.RotateLeft64(x, -int(count))
	}
}

func divideResult(width, op int, x, y uint64) uint64 {
	if width == 32 {
		switch op {
		case 0:
			return uint64(uint32(int32(x) / int32(y)))
		case 1:
			return uint64(uint32(x) / uint32(y))
		case 2:
			return uint64(uint32(int32(x) % int32(y)))
		default:
			return uint64(uint32(x) % uint32(y))
		}
	}
	switch op {
	case 0:
		return uint64(int64(x) / int64(y))
	case 1:
		return x / y
	case 2:
		return uint64(int64(x) % int64(y))
	default:
		return x % y
	}
}

func TestI64ShiftCountBoundaries(t *testing.T) {
	for op, name := range []string{"shl", "shr_s", "shr_u", "rotl", "rotr"} {
		t.Run(name, func(t *testing.T) {
			m := mod1(t, []wasm.ValType{i64, i64}, []wasm.ValType{i64}, []byte{0, 0x20, 0, 0x20, 1, byte(0x86 + op), 0x0b})
			cm, err := CompileModule(m)
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			for _, count := range shiftBoundaryCounts {
				for _, x := range []uint64{0x8123456789abcdef, 0x7123456789abcdef} {
					if got, want := runCompiledAmd64u(t, cm, x, count), shiftResult(64, op, x, count); got != want {
						t.Fatalf("x=%#x count=%d: got %#x, want %#x", x, count, got, want)
					}
				}
			}
		})
	}
}

func TestNestedShiftDivisionPressure(t *testing.T) {
	const depth = 24
	for _, width := range []int{32, 64} {
		typ, constant, xor, divBase, shiftBase := i32, byte(0x41), byte(0x73), byte(0x6d), byte(0x74)
		if width == 64 {
			typ, constant, xor, divBase, shiftBase = i64, 0x42, 0x85, 0x7f, 0x86
		}
		for div, divName := range []string{"div_s", "div_u", "rem_s", "rem_u"} {
			for shift, shiftName := range []string{"shl", "shr_s", "shr_u", "rotl", "rotr"} {
				t.Run(fmt.Sprintf("i%d/%s/%s", width, divName, shiftName), func(t *testing.T) {
					body := []byte{0}
					// Each left operand stays live while the next count is evaluated.
					// Variable division uses RAX/RDX; the nested shifts also need RCX.
					for level := 0; level < depth; level++ {
						body = append(body, 0x20, 0, constant, byte(level+1), xor, 0x20, 2, divBase+byte(div))
					}
					body = append(body, 0x20, 1)
					for level := depth - 1; level >= 0; level-- {
						body = append(body, shiftBase+byte((shift+level)%5), 0x20, 3, xor)
					}
					body = append(body, 0x0b)
					m := mod1(t, []wasm.ValType{typ, typ, typ, typ}, []wasm.ValType{typ}, body)
					stats := &ModuleStats{}
					cm, err := CompileModuleWith(m, CompileOptions{Stats: stats})
					if err != nil {
						t.Fatal(err)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					s := stats.Funcs[0]
					if s.Spills == 0 || s.Reloads == 0 {
						t.Fatalf("fixture did not spill and reload: %+v", s)
					}
					t.Logf("code=%d frame=%d spills=%d reloads=%d", s.CodeBytes, s.FrameBytes, s.Spills, s.Reloads)
					for _, count := range shiftBoundaryCounts {
						for _, divisor := range []uint64{3, ^uint64(2)} {
							x, salt := uint64(0x8123456789abcdef), uint64(0x63d27a19)
							want := count
							for level := depth - 1; level >= 0; level-- {
								left := divideResult(width, div, x^uint64(level+1), divisor)
								want = shiftResult(width, (shift+level)%5, left, want) ^ salt
							}
							got := runCompiledAmd64u(t, cm, x, count, divisor, salt)
							if width == 32 {
								got = uint64(uint32(got))
							}
							if got != want {
								t.Fatalf("count=%d divisor=%#x: got %#x, want %#x", count, divisor, got, want)
							}
						}
					}
				})
			}
		}
	}
}
