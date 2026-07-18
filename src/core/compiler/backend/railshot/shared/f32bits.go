package shared

import (
	"encoding/binary"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// TranslateF32BitBody maps the allocation-free raw-bit f32 subset to the
// already-qualified i32 scalar compiler. One hidden i32 local holds the sign
// operand for copysign; all declared f32 locals become raw i32 homes.
func TranslateF32BitBody(numParams int, body []byte) ([]byte, error) {
	r := wasm.NewReader(body)
	groups, err := r.U32()
	if err != nil {
		return nil, err
	}
	out := appendULEB32(nil, groups+1)
	localCount := 0
	for i := uint32(0); i < groups; i++ {
		n, err := r.U32()
		if err != nil {
			return nil, err
		}
		typ, err := r.Byte()
		if err != nil {
			return nil, err
		}
		if typ != byte(wasm.NumF32) {
			return nil, fmt.Errorf("f32 bit function local type %#x", typ)
		}
		out = appendULEB32(out, n)
		out = append(out, byte(wasm.NumI32))
		localCount += int(n)
	}
	out = appendULEB32(out, 1)
	out = append(out, byte(wasm.NumI32))
	temp := uint32(numParams + localCount)
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return nil, err
		}
		switch op {
		case 0x01, 0x0b, 0x1a:
			out = append(out, op)
			if op == 0x0b {
				if r.HasNext() {
					return nil, fmt.Errorf("f32 bit function has instructions after end")
				}
				return out, nil
			}
		case 0x20, 0x21, 0x22:
			idx, err := r.U32()
			if err != nil {
				return nil, err
			}
			out = append(out, op)
			out = appendULEB32(out, idx)
		case 0x43:
			bits, err := r.Bytes(4)
			if err != nil {
				return nil, err
			}
			out = append(out, 0x41)
			out = appendSLEB32(out, int32(binary.LittleEndian.Uint32(bits)))
		case 0x8b: // abs
			out = append(out, 0x41)
			out = appendSLEB32(out, 0x7fffffff)
			out = append(out, 0x71)
		case 0x8c: // neg
			out = append(out, 0x41)
			out = appendSLEB32(out, -0x80000000)
			out = append(out, 0x73)
		case 0x98: // copysign
			out = append(out, 0x21)
			out = appendULEB32(out, temp)
			out = append(out, 0x41)
			out = appendSLEB32(out, 0x7fffffff)
			out = append(out, 0x71, 0x20)
			out = appendULEB32(out, temp)
			out = append(out, 0x41)
			out = appendSLEB32(out, -0x80000000)
			out = append(out, 0x71, 0x72)
		default:
			return nil, fmt.Errorf("f32 bit function unsupported opcode %#x", op)
		}
	}
	return nil, fmt.Errorf("f32 bit function missing end")
}

func appendSLEB32(dst []byte, value int32) []byte {
	for {
		b := byte(value & 0x7f)
		value >>= 7
		done := (value == 0 && b&0x40 == 0) || (value == -1 && b&0x40 != 0)
		if !done {
			b |= 0x80
		}
		dst = append(dst, b)
		if done {
			return dst
		}
	}
}
