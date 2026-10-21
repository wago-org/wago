//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestBitfieldLocalSinkRegisters(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, c, sub, shr, and := wasm.I32, byte(0x41), byte(0x6b), byte(0x76), byte(0x71)
		if wide {
			typ, c, sub, shr, and = wasm.I64, 0x42, 0x7d, 0x88, 0x83
		}
		body := []byte{0, 0x3f, 0, 0x1a, 0x20, 0, c, 1, sub, c, 11, shr, c, 31, and, 0x22, 0, 0x0b}
		m := mod1(t, []wasm.ValType{typ}, []wasm.ValType{typ}, body)
		m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
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
			if word&0x7f800000 == 0x53000000 && word>>16&63 == 11 && word>>10&63 == 15 {
				found = true
				if word&31 != word>>5&31 {
					t.Fatalf("wide=%v: extract copies arithmetic scratch into local: %08x", wide, word)
				}
			}
		}
		if !found {
			t.Fatal("missing bounded extract")
		}
		for _, x := range []uint64{0, 1, 2048, 65536, 0x80000000, 0x8000000000000000, ^uint64(0)} {
			v := x - 1
			if !wide {
				v = uint64(uint32(v))
			}
			want := v >> 11 & 31
			got, err := runArm64WrapperWithOptions(t, m, CompileOptions{}, x)
			if err != nil || got != want {
				t.Fatalf("wide=%v x=%x got=%x want=%x err=%v", wide, x, got, want, err)
			}
		}
	}
}

func TestBitfieldLocalSinkOldRightSource(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, c, sub, shr, and, xor := wasm.I32, byte(0x41), byte(0x6b), byte(0x76), byte(0x71), byte(0x73)
		if wide {
			typ, c, sub, shr, and, xor = wasm.I64, 0x42, 0x7d, 0x88, 0x83, 0x85
		}
		for _, op := range []byte{sub, xor} {
			body := []byte{0, 0x3f, 0, 0x1a, 0x20, 0, 0x20, 1, 0x20, 0, op, c, 11, shr, c, 31, and, 0x22, 0, xor, 0x0b}
			m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, body)
			m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
			for _, x := range []uint64{0, 1, 0x80000000, ^uint64(0)} {
				for _, y := range []uint64{0, 1, 65536, ^uint64(0)} {
					v := y - x
					if op == xor {
						v = y ^ x
					}
					if !wide {
						v = uint64(uint32(v))
					}
					want := (v >> 11 & 31) ^ x
					if !wide {
						want = uint64(uint32(want))
					}
					got, err := runArm64WrapperWithOptions(t, m, CompileOptions{}, x, y)
					if !wide {
						got = uint64(uint32(got))
					}
					if err != nil || got != want {
						t.Fatalf("wide=%v op=%x x=%x y=%x got=%x want=%x err=%v", wide, op, x, y, got, want, err)
					}
				}
			}
		}
	}
}
