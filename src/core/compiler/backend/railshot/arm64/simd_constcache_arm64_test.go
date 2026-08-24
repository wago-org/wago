//go:build (linux || darwin) && arm64

package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestSIMDV128ConstSplatMaterialization(t *testing.T) {
	tests := []struct {
		name string
		v    [16]byte
		base uint32
	}{
		{name: "byte", v: i8x16Bytes(-85, -85, -85, -85, -85, -85, -85, -85, -85, -85, -85, -85, -85, -85, -85, -85), base: 0x4e010c00},
		{name: "half", v: i16x8Bytes(0x1234, 0x1234, 0x1234, 0x1234, 0x1234, 0x1234, 0x1234, 0x1234), base: 0x4e020c00},
		{name: "word", v: i32x4Bytes(0x12345678, 0x12345678, 0x12345678, 0x12345678), base: 0x4e040c00},
		{name: "dword", v: i64x2Bytes(0x0123456789abcdef, 0x0123456789abcdef), base: 0x4e080c00},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := append([]byte{0x00}, simdConst(tc.v)...)
			body = append(body, simdOp(29)...) // i64x2.extract_lane
			body = append(body, 0x00, 0x0b)    // lane 0; end
			m := mod1(t, nil, []wasm.ValType{wasm.I64}, body)
			cm, err := CompileModule(m)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			found := false
			for at := 0; at+4 <= len(cm.Code); at += 4 {
				word := binary.LittleEndian.Uint32(cm.Code[at:])
				if word&0xfffffc00 == tc.base {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("compiled code does not contain expected DUP lane form %#08x", tc.base)
			}
			if got, want := runArm64u(t, m), binary.LittleEndian.Uint64(tc.v[:8]); got != want {
				t.Fatalf("result %#016x, want %#016x", got, want)
			}
		})
	}
}
