package wasm3

import (
	"encoding/binary"
	"fmt"
)

var simpleKindOpcode map[InstrKind]byte
var memKindOpcode map[InstrKind]byte

func init() {
	simpleKindOpcode = make(map[InstrKind]byte, len(simpleOpcode))
	for op, k := range simpleOpcode {
		simpleKindOpcode[k] = op
	}
	memKindOpcode = make(map[InstrKind]byte, len(memOpcodeKind))
	for op, k := range memOpcodeKind {
		memKindOpcode[k] = op
	}
}

// EncodeExpr serializes a decoded expression back to canonical wasm bytecode,
// including the terminating end opcode. The backend uses this after wasm3
// validation/support filtering so it can keep its existing byte-oriented
// single-pass code generator while wasm3 remains the sole decoder/validator.
func EncodeExpr(e Expr) ([]byte, error) {
	var out []byte
	if err := appendInstrs(&out, e.Instrs); err != nil {
		return nil, err
	}
	out = append(out, 0x0b)
	return out, nil
}

func appendInstrs(out *[]byte, instrs []Instruction) error {
	for _, in := range instrs {
		if err := appendInstr(out, in); err != nil {
			return err
		}
	}
	return nil
}

func appendInstr(out *[]byte, in Instruction) error {
	if op, ok := simpleKindOpcode[in.Kind]; ok {
		*out = append(*out, op)
		return nil
	}
	if op, ok := memKindOpcode[in.Kind]; ok {
		*out = append(*out, op)
		appendU32(out, in.MemArg.Align)
		appendU64AsU32(out, in.MemArg.Offset)
		return nil
	}
	switch in.Kind {
	case InstrBlock:
		*out = append(*out, 0x02)
		if err := appendBlockType(out, in.BlockType); err != nil {
			return err
		}
		if err := appendInstrs(out, in.Body.Instrs); err != nil {
			return err
		}
		*out = append(*out, 0x0b)
	case InstrLoop:
		*out = append(*out, 0x03)
		if err := appendBlockType(out, in.BlockType); err != nil {
			return err
		}
		if err := appendInstrs(out, in.Body.Instrs); err != nil {
			return err
		}
		*out = append(*out, 0x0b)
	case InstrIf:
		*out = append(*out, 0x04)
		if err := appendBlockType(out, in.BlockType); err != nil {
			return err
		}
		if err := appendInstrs(out, in.Then); err != nil {
			return err
		}
		if len(in.Else) != 0 {
			*out = append(*out, 0x05)
			if err := appendInstrs(out, in.Else); err != nil {
				return err
			}
		}
		*out = append(*out, 0x0b)
	case InstrBr:
		*out = append(*out, 0x0c)
		appendU32(out, in.Index)
	case InstrBrIf:
		*out = append(*out, 0x0d)
		appendU32(out, in.Index)
	case InstrBrTable:
		*out = append(*out, 0x0e)
		appendU32(out, uint32(len(in.Indices)))
		for _, idx := range in.Indices {
			appendU32(out, idx)
		}
		appendU32(out, in.Index)
	case InstrCall:
		*out = append(*out, 0x10)
		appendU32(out, in.Index)
	case InstrCallIndirect:
		*out = append(*out, 0x11)
		appendU32(out, in.Index)
		appendU32(out, in.Index2)
	case InstrSelect:
		if len(in.ValTypes) == 0 {
			*out = append(*out, 0x1b)
			return nil
		}
		*out = append(*out, 0x1c)
		appendU32(out, uint32(len(in.ValTypes)))
		for _, vt := range in.ValTypes {
			b, ok := EncodeValType(vt)
			if !ok {
				return fmt.Errorf("wasm3 encode: unsupported select value type %s", vt)
			}
			*out = append(*out, b)
		}
	case InstrLocalGet:
		*out = append(*out, 0x20)
		appendU32(out, in.Index)
	case InstrLocalSet:
		*out = append(*out, 0x21)
		appendU32(out, in.Index)
	case InstrLocalTee:
		*out = append(*out, 0x22)
		appendU32(out, in.Index)
	case InstrGlobalGet:
		*out = append(*out, 0x23)
		appendU32(out, in.Index)
	case InstrGlobalSet:
		*out = append(*out, 0x24)
		appendU32(out, in.Index)
	case InstrMemorySize:
		*out = append(*out, 0x3f, 0x00)
	case InstrMemoryGrow:
		*out = append(*out, 0x40, 0x00)
	case InstrI32Const:
		*out = append(*out, 0x41)
		appendS32(out, in.I32)
	case InstrI64Const:
		*out = append(*out, 0x42)
		appendS64(out, in.I64)
	case InstrF32Const:
		*out = append(*out, 0x43)
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], in.F32Bits)
		*out = append(*out, b[:]...)
	case InstrF64Const:
		*out = append(*out, 0x44)
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], in.F64Bits)
		*out = append(*out, b[:]...)
	case InstrMemoryCopy:
		*out = append(*out, 0xfc)
		appendU32(out, 10)
		appendU32(out, in.Index)
		appendU32(out, in.Index2)
	case InstrMemoryFill:
		*out = append(*out, 0xfc)
		appendU32(out, 11)
		appendU32(out, in.Index)
	default:
		return fmt.Errorf("wasm3 encode: unsupported instruction %s", in.Kind)
	}
	return nil
}

func appendBlockType(out *[]byte, bt BlockType) error {
	switch bt.Kind {
	case BlockVoid:
		*out = append(*out, 0x40)
	case BlockVal:
		b, ok := EncodeValType(bt.Val)
		if !ok {
			return fmt.Errorf("wasm3 encode: unsupported block value type %s", bt.Val)
		}
		*out = append(*out, b)
	case BlockTypeIndex:
		if bt.Type.Rec {
			return fmt.Errorf("wasm3 encode: recursive block type %d", bt.Type.Index)
		}
		appendS64(out, int64(bt.Type.Index))
	default:
		return fmt.Errorf("wasm3 encode: invalid block type")
	}
	return nil
}

func appendU64AsU32(out *[]byte, v uint64) {
	// Support pass rejects memory64/multi-memory before codegen; MVP memargs are u32.
	appendU32(out, uint32(v))
}

func appendU32(out *[]byte, v uint32) {
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		*out = append(*out, b)
		if v == 0 {
			return
		}
	}
}

func appendS32(out *[]byte, v int32) { appendS64(out, int64(v)) }

func appendS64(out *[]byte, v int64) {
	more := true
	for more {
		b := byte(v & 0x7f)
		v >>= 7
		sign := b&0x40 != 0
		more = !((v == 0 && !sign) || (v == -1 && sign))
		if more {
			b |= 0x80
		}
		*out = append(*out, b)
	}
}
