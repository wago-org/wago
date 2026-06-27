package wasm3

const maxInstructionNestingDepth = 20000

func decodeBlockType(r *reader) (BlockType, error) {
	b, ok := r.peek()
	if !ok {
		return BlockType{}, &DecodeError{Code: ErrInvalidBlockType, Offset: r.off()}
	}
	if b == 0x40 {
		_, _ = r.byte()
		return BlockType{Kind: BlockVoid}, nil
	}
	if isValTypeLead(b) {
		vt, err := decodeValType(r)
		return BlockType{Kind: BlockVal, Val: vt}, err
	}
	x, err := r.s33()
	if err != nil || x < 0 {
		return BlockType{}, &DecodeError{Code: ErrInvalidBlockType, Offset: r.off()}
	}
	return BlockType{Kind: BlockTypeIndex, Type: TypeIdx{Index: uint32(x)}}, nil
}

func isValTypeLead(b byte) bool {
	switch b {
	case 0x7f, 0x7e, 0x7d, 0x7c, 0x7b, 0x63, 0x64, 0x6f, 0x70, 0x6e, 0x6d, 0x6c, 0x6b, 0x6a, 0x69, 0x71, 0x72, 0x73, 0x74:
		return true
	}
	return false
}

func decodeExpr(r *reader, depth int) (Expr, error) {
	if depth > maxInstructionNestingDepth {
		return Expr{}, &DecodeError{Code: ErrInstructionNestingLimitExceeded, Offset: r.off()}
	}
	var instrs []Instruction
	for {
		b, ok := r.peek()
		if !ok {
			return Expr{}, &DecodeError{Code: ErrIndexOutOfBounds, Offset: r.off()}
		}
		if b == 0x0b {
			_, _ = r.byte()
			return Expr{Instrs: instrs}, nil
		}
		inst, err := decodeInstruction(r, depth+1)
		if err != nil {
			return Expr{}, err
		}
		instrs = append(instrs, inst)
	}
}

func decodeInstruction(r *reader, depth int) (Instruction, error) {
	if depth > maxInstructionNestingDepth {
		return Instruction{}, &DecodeError{Code: ErrInstructionNestingLimitExceeded, Offset: r.off()}
	}
	op, err := r.byte()
	if err != nil {
		return Instruction{}, err
	}
	if k := simpleOpcode[op]; k != InstrInvalid {
		return Instruction{Kind: k}, nil
	}
	switch op {
	case 0x02, 0x03, 0x04:
		bt, err := decodeBlockType(r)
		if err != nil {
			return Instruction{}, err
		}
		if op == 0x04 {
			thenInstrs, elseInstrs, err := decodeIfBodies(r, depth+1)
			return Instruction{Kind: InstrIf, ext: &instrExt{BlockType: bt, Then: thenInstrs, Else: elseInstrs}}, err
		}
		body, err := decodeExpr(r, depth+1)
		if err != nil {
			return Instruction{}, err
		}
		if op == 0x02 {
			return Instruction{Kind: InstrBlock, ext: &instrExt{BlockType: bt, Body: body}}, nil
		}
		return Instruction{Kind: InstrLoop, ext: &instrExt{BlockType: bt, Body: body}}, nil
	case 0x08:
		return indexInst(r, InstrThrow)
	case 0x0c:
		return indexInst(r, InstrBr)
	case 0x0d:
		return indexInst(r, InstrBrIf)
	case 0x0e:
		labels, err := readVec(r, func(r *reader) (uint32, error) { return r.u32() })
		if err != nil {
			return Instruction{}, err
		}
		def, err := r.u32()
		if err != nil {
			return Instruction{}, err
		}
		return Instruction{Kind: InstrBrTable, Index: def, ext: &instrExt{Indices: labels}}, nil
	case 0x10:
		return indexInst(r, InstrCall)
	case 0x11:
		return twoIndexInst(r, InstrCallIndirect)
	case 0x12:
		return indexInst(r, InstrReturnCall)
	case 0x13:
		return twoIndexInst(r, InstrReturnCallIndirect)
	case 0x14:
		return indexInst(r, InstrCallRef)
	case 0x15:
		return indexInst(r, InstrReturnCallRef)
	case 0x1c:
		vts, err := decodeResultType(r)
		return Instruction{Kind: InstrSelect, ext: &instrExt{ValTypes: vts}}, err
	case 0x1f:
		bt, err := decodeBlockType(r)
		if err != nil {
			return Instruction{}, err
		}
		catches, err := readVec(r, decodeCatch)
		if err != nil {
			return Instruction{}, err
		}
		body, err := decodeExpr(r, depth+1)
		return Instruction{Kind: InstrTryTable, ext: &instrExt{BlockType: bt, Catches: catches, Body: body}}, err
	case 0x20:
		return indexInst(r, InstrLocalGet)
	case 0x21:
		return indexInst(r, InstrLocalSet)
	case 0x22:
		return indexInst(r, InstrLocalTee)
	case 0x23:
		return indexInst(r, InstrGlobalGet)
	case 0x24:
		return indexInst(r, InstrGlobalSet)
	case 0x25:
		return indexInst(r, InstrTableGet)
	case 0x26:
		return indexInst(r, InstrTableSet)
	case 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e:
		ma, err := decodeMemArg(r)
		return Instruction{Kind: memOpcodeKind[op], ext: &instrExt{MemArg: ma}}, err
	case 0x3f:
		return memidxInst(r, InstrMemorySize)
	case 0x40:
		return memidxInst(r, InstrMemoryGrow)
	case 0x41:
		x, err := r.i32()
		return Instruction{Kind: InstrI32Const, I32: x}, err
	case 0x42:
		x, err := r.i64()
		return Instruction{Kind: InstrI64Const, I64: x}, err
	case 0x43:
		x, err := r.le32()
		return Instruction{Kind: InstrF32Const, F32Bits: x}, err
	case 0x44:
		x, err := r.le64()
		return Instruction{Kind: InstrF64Const, F64Bits: x}, err
	case 0xd0:
		rt, err := decodeRefTypeForNull(r)
		return Instruction{Kind: InstrRefNull, ext: &instrExt{RefType: rt}}, err
	case 0xd2:
		return indexInst(r, InstrRefFunc)
	case 0xd3:
		return Instruction{Kind: InstrRefEq}, nil
	case 0xd4:
		return Instruction{Kind: InstrRefAsNonNull}, nil
	case 0xd5:
		return indexInst(r, InstrBrOnNull)
	case 0xd6:
		return indexInst(r, InstrBrOnNonNull)
	case 0xfb:
		return decodeFB(r)
	case 0xfc:
		return decodeFC(r)
	case 0xfd:
		return decodeFD(r)
	case 0xfe:
		return decodeFE(r)
	default:
		return Instruction{}, &DecodeError{Code: ErrInvalidInstruction, Offset: r.off() - 1}
	}
}

func decodeIfBodies(r *reader, depth int) ([]Instruction, []Instruction, error) {
	var thenBody, elseBody []Instruction
	inElse := false
	for {
		b, ok := r.peek()
		if !ok {
			return nil, nil, &DecodeError{Code: ErrIndexOutOfBounds, Offset: r.off()}
		}
		if b == 0x05 {
			if inElse {
				return nil, nil, &DecodeError{Code: ErrInvalidInstruction, Offset: r.off()}
			}
			_, _ = r.byte()
			inElse = true
			continue
		}
		if b == 0x0b {
			_, _ = r.byte()
			return thenBody, elseBody, nil
		}
		inst, err := decodeInstruction(r, depth+1)
		if err != nil {
			return nil, nil, err
		}
		if inElse {
			elseBody = append(elseBody, inst)
		} else {
			thenBody = append(thenBody, inst)
		}
	}
}

func indexInst(r *reader, k InstrKind) (Instruction, error) {
	x, err := r.u32()
	return Instruction{Kind: k, Index: x}, err
}
func twoIndexInst(r *reader, k InstrKind) (Instruction, error) {
	a, err := r.u32()
	if err != nil {
		return Instruction{}, err
	}
	b, err := r.u32()
	return Instruction{Kind: k, Index: a, Index2: b}, err
}
func memidxInst(r *reader, k InstrKind) (Instruction, error) {
	x, err := r.u32()
	return Instruction{Kind: k, Index: x}, err
}
func decodeMemArg(r *reader) (MemArg, error) {
	n, err := r.u32()
	if err != nil {
		return MemArg{}, err
	}
	ma := MemArg{}
	if n >= 64 && n < 128 {
		ma.Align = n - 64
		mi, err := r.u32()
		if err != nil {
			return ma, err
		}
		m := MemIdx(mi)
		ma.Mem = &m
	} else if n < 64 {
		ma.Align = n
	} else {
		return ma, &DecodeError{Code: ErrInvalidInstruction, Offset: r.off()}
	}
	off, err := r.u64()
	ma.Offset = off
	return ma, err
}
func decodeCatch(r *reader) (Catch, error) {
	b, err := r.byte()
	if err != nil {
		return Catch{}, err
	}
	switch b {
	case 0, 1:
		t, err := r.u32()
		if err != nil {
			return Catch{}, err
		}
		l, err := r.u32()
		return Catch{Kind: CatchKind(b), Tag: TagIdx(t), Label: LabelIdx(l)}, err
	case 2, 3:
		l, err := r.u32()
		return Catch{Kind: CatchKind(b), Label: LabelIdx(l)}, err
	default:
		return Catch{}, &DecodeError{Code: ErrInvalidInstruction, Offset: r.off() - 1}
	}
}
func decodeRefTypeForNull(r *reader) (RefType, error) {
	exact, ht, err := decodeRefHeapType(r)
	if err != nil {
		return RefType{}, err
	}
	return Ref(true, ht, exact), nil
}

// simpleOpcode and memOpcodeKind are dense [256]InstrKind tables indexed by the
// raw opcode byte. A zero entry (InstrInvalid) means "not in this table". Arrays
// avoid the map hashing that otherwise dominates the decode hot loop.
var simpleOpcode = [256]InstrKind{
	0x00: InstrUnreachable, 0x01: InstrNop, 0x0a: InstrThrowRef, 0x0f: InstrReturn, 0x1a: InstrDrop, 0x1b: InstrSelect, 0xd1: InstrRefIsNull,
	0x45: InstrI32Eqz, 0x46: InstrI32Eq, 0x47: InstrI32Ne, 0x48: InstrI32LtS, 0x49: InstrI32LtU, 0x4a: InstrI32GtS, 0x4b: InstrI32GtU, 0x4c: InstrI32LeS, 0x4d: InstrI32LeU, 0x4e: InstrI32GeS, 0x4f: InstrI32GeU,
	0x50: InstrI64Eqz, 0x51: InstrI64Eq, 0x52: InstrI64Ne, 0x53: InstrI64LtS, 0x54: InstrI64LtU, 0x55: InstrI64GtS, 0x56: InstrI64GtU, 0x57: InstrI64LeS, 0x58: InstrI64LeU, 0x59: InstrI64GeS, 0x5a: InstrI64GeU,
	0x5b: InstrF32Eq, 0x5c: InstrF32Ne, 0x5d: InstrF32Lt, 0x5e: InstrF32Gt, 0x5f: InstrF32Le, 0x60: InstrF32Ge, 0x61: InstrF64Eq, 0x62: InstrF64Ne, 0x63: InstrF64Lt, 0x64: InstrF64Gt, 0x65: InstrF64Le, 0x66: InstrF64Ge,
	0x67: InstrI32Clz, 0x68: InstrI32Ctz, 0x69: InstrI32Popcnt, 0x6a: InstrI32Add, 0x6b: InstrI32Sub, 0x6c: InstrI32Mul, 0x6d: InstrI32DivS, 0x6e: InstrI32DivU, 0x6f: InstrI32RemS, 0x70: InstrI32RemU, 0x71: InstrI32And, 0x72: InstrI32Or, 0x73: InstrI32Xor, 0x74: InstrI32Shl, 0x75: InstrI32ShrS, 0x76: InstrI32ShrU, 0x77: InstrI32Rotl, 0x78: InstrI32Rotr,
	0x79: InstrI64Clz, 0x7a: InstrI64Ctz, 0x7b: InstrI64Popcnt, 0x7c: InstrI64Add, 0x7d: InstrI64Sub, 0x7e: InstrI64Mul, 0x7f: InstrI64DivS, 0x80: InstrI64DivU, 0x81: InstrI64RemS, 0x82: InstrI64RemU, 0x83: InstrI64And, 0x84: InstrI64Or, 0x85: InstrI64Xor, 0x86: InstrI64Shl, 0x87: InstrI64ShrS, 0x88: InstrI64ShrU, 0x89: InstrI64Rotl, 0x8a: InstrI64Rotr,
	0x8b: InstrF32Abs, 0x8c: InstrF32Neg, 0x8d: InstrF32Ceil, 0x8e: InstrF32Floor, 0x8f: InstrF32Trunc, 0x90: InstrF32Nearest, 0x91: InstrF32Sqrt, 0x92: InstrF32Add, 0x93: InstrF32Sub, 0x94: InstrF32Mul, 0x95: InstrF32Div, 0x96: InstrF32Min, 0x97: InstrF32Max, 0x98: InstrF32Copysign,
	0x99: InstrF64Abs, 0x9a: InstrF64Neg, 0x9b: InstrF64Ceil, 0x9c: InstrF64Floor, 0x9d: InstrF64Trunc, 0x9e: InstrF64Nearest, 0x9f: InstrF64Sqrt, 0xa0: InstrF64Add, 0xa1: InstrF64Sub, 0xa2: InstrF64Mul, 0xa3: InstrF64Div, 0xa4: InstrF64Min, 0xa5: InstrF64Max, 0xa6: InstrF64Copysign,
	0xa7: InstrI32WrapI64, 0xa8: InstrI32TruncF32S, 0xa9: InstrI32TruncF32U, 0xaa: InstrI32TruncF64S, 0xab: InstrI32TruncF64U, 0xac: InstrI64ExtendI32S, 0xad: InstrI64ExtendI32U, 0xae: InstrI64TruncF32S, 0xaf: InstrI64TruncF32U, 0xb0: InstrI64TruncF64S, 0xb1: InstrI64TruncF64U, 0xb2: InstrF32ConvertI32S, 0xb3: InstrF32ConvertI32U, 0xb4: InstrF32ConvertI64S, 0xb5: InstrF32ConvertI64U, 0xb6: InstrF32DemoteF64, 0xb7: InstrF64ConvertI32S, 0xb8: InstrF64ConvertI32U, 0xb9: InstrF64ConvertI64S, 0xba: InstrF64ConvertI64U, 0xbb: InstrF64PromoteF32, 0xbc: InstrI32ReinterpretF32, 0xbd: InstrI64ReinterpretF64, 0xbe: InstrF32ReinterpretI32, 0xbf: InstrF64ReinterpretI64,
	0xc0: InstrI32Extend8S, 0xc1: InstrI32Extend16S, 0xc2: InstrI64Extend8S, 0xc3: InstrI64Extend16S, 0xc4: InstrI64Extend32S,
}

var memOpcodeKind = [256]InstrKind{0x28: InstrI32Load, 0x29: InstrI64Load, 0x2a: InstrF32Load, 0x2b: InstrF64Load, 0x2c: InstrI32Load8S, 0x2d: InstrI32Load8U, 0x2e: InstrI32Load16S, 0x2f: InstrI32Load16U, 0x30: InstrI64Load8S, 0x31: InstrI64Load8U, 0x32: InstrI64Load16S, 0x33: InstrI64Load16U, 0x34: InstrI64Load32S, 0x35: InstrI64Load32U, 0x36: InstrI32Store, 0x37: InstrI64Store, 0x38: InstrF32Store, 0x39: InstrF64Store, 0x3a: InstrI32Store8, 0x3b: InstrI32Store16, 0x3c: InstrI64Store8, 0x3d: InstrI64Store16, 0x3e: InstrI64Store32}
