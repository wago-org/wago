//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestScalarFloatSelectBorrowedRegisters(t *testing.T) {
	for _, wide := range []bool{false, true} {
		ft, it, reinterpret := wasm.F32, wasm.I32, byte(0xbc)
		if wide {
			ft, it, reinterpret = wasm.F64, wasm.I64, 0xbd
		}
		for _, alias := range []bool{false, true} {
			second := byte(1)
			if alias {
				second = 0
			}
			body := []byte{0, 0x20, 0, 0x20, second, 0x20, 2, 0x45, 0x1b, reinterpret, 0x0b}
			m := mod1(t, []wasm.ValType{ft, ft, wasm.I32}, []wasm.ValType{it}, body)
			cm, err := CompileModuleWith(m, CompileOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			found := false
			for at := 0; at+4 <= len(cm.Code); at += 4 {
				word := binary.LittleEndian.Uint32(cm.Code[at:])
				if word&0xff200c00 == 0x1e200c00 {
					found = true
					// Parameter homes are v8/v9. The select reads them directly.
					if word>>5&31 != 8 || word>>16&31 != uint32(8+second) {
						t.Fatalf("wide=%v alias=%v copied FP operands before FCSEL: %08x", wide, alias, word)
					}
				}
			}
			if !found {
				t.Fatal("missing FCSEL")
			}
			a, b := uint64(0x7f812345), uint64(0x80000000)
			if wide {
				a, b = 0x7ff0000000001234, 0x8000000000000000
			}
			for _, cond := range []uint64{0, 1, 1 << 32, (1 << 32) | 1} {
				want := b
				if uint32(cond) == 0 || alias {
					want = a
				}
				got, err := runArm64WrapperWithOptions(t, m, CompileOptions{}, a, b, cond)
				if !wide {
					got = uint64(uint32(got))
				}
				if err != nil || got != want {
					t.Fatalf("wide=%v alias=%v cond=%x got=%x want=%x err=%v", wide, alias, cond, got, want, err)
				}
			}
		}
	}
}

func TestScalarFloatSelectCachedConstants(t *testing.T) {
	for _, wide := range []bool{false, true} {
		c, reinterpret, it := byte(0x43), byte(0xbc), wasm.I32
		a, b := uint64(0x7f812345), uint64(0x80000000)
		if wide {
			c, reinterpret, it, a, b = 0x44, 0xbd, wasm.I64, 0x7ff0000000001234, 0x8000000000000000
		}
		body := []byte{0}
		for _, value := range []uint64{a, b} {
			body = append(body, c)
			var bits [8]byte
			binary.LittleEndian.PutUint64(bits[:], value)
			n := 4
			if wide {
				n = 8
			}
			body = append(body, bits[:n]...)
		}
		body = append(body, 0x20, 0, 0x45, 0x1b, reinterpret, 0x0b)
		m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{it}, body)
		for _, condition := range []uint64{0, 1, 1 << 32} {
			want := b
			if uint32(condition) == 0 {
				want = a
			}
			got, err := runArm64WrapperWithOptions(t, m, CompileOptions{}, condition)
			if !wide {
				got = uint64(uint32(got))
			}
			if err != nil || got != want {
				t.Fatalf("wide=%v condition=%x got=%x want=%x err=%v", wide, condition, got, want, err)
			}
		}
	}
}
