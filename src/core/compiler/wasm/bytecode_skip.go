package wasm

// skipExprOp skips exactly one already-prefixed instruction from r and returns
// its structural kind. It is allocation-free for vector immediates such as
// br_table labels and try_table catches.
func skipExprOp(r *reader) (directOpKind, error) {
	op, err := r.byte()
	if err != nil {
		return directInstr, err
	}
	return skipExprOpAfterOpcode(r, op)
}

func skipExprOpAfterOpcode(r *reader, op byte) (directOpKind, error) {
	var scratch InstructionImmediate
	return classifyExprOpAfterOpcode(r, op, &scratch)
}

// classifyExprOpAfterOpcode consumes op's immediates from r and writes the opcode
// metadata into *imm, returning the structural kind. It writes through a pointer
// to avoid copying the InstructionImmediate on this compile hot path. imm must be
// zero-valued on entry; error paths leave it unchanged (i.e. zero).
func classifyExprOpAfterOpcode(r *reader, op byte, imm *InstructionImmediate) (directOpKind, error) {
	return classifyExprOpAfterOpcodeWithMemarg64(r, op, imm, false)
}

func classifyExprOpAfterOpcodeWithMemarg64(r *reader, op byte, imm *InstructionImmediate, memarg64 bool) (directOpKind, error) {
	return classifyExprOpAfterOpcodeWithFeatures(r, op, imm, memarg64, false)
}

func classifyExprOpAfterOpcodeWithFeatures(r *reader, op byte, imm *InstructionImmediate, memarg64, multiMemory bool) (directOpKind, error) {
	if k := simpleOpcode[op]; k != InstrInvalid {
		imm.Kind = k
		return directInstr, nil
	}
	switch op {
	case 0x02, 0x03, 0x04:
		if _, err := decodeBlockType(r); err != nil {
			return directInstr, err
		}
		switch op {
		case 0x02:
			imm.Kind = InstrBlock
			return directBlock, nil
		case 0x03:
			imm.Kind = InstrLoop
			return directLoop, nil
		default:
			imm.Kind = InstrIf
			return directIf, nil
		}
	case 0x05:
		return directElse, nil
	case 0x08, 0x0c, 0x0d, 0x10, 0x12, 0x14, 0x15, 0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0xd2, 0xd5, 0xd6:
		idx, err := r.u32()
		imm.Kind, imm.Index = oneIndexImmediateKind(op), idx
		return directInstr, err
	case 0x0b:
		return directEnd, nil
	case 0x0e:
		n, err := r.u32()
		if err != nil {
			return directInstr, err
		}
		for i := uint32(0); i < n; i++ {
			if _, err := r.u32(); err != nil {
				return directInstr, err
			}
		}
		idx, err := r.u32()
		imm.Kind, imm.Index = InstrBrTable, idx
		return directInstr, err
	case 0x11, 0x13:
		idx, err := r.u32()
		if err != nil {
			return directInstr, err
		}
		idx2, err := r.u32()
		imm.Kind, imm.Index, imm.Index2 = InstrCallIndirect, idx, idx2
		if op == 0x13 {
			imm.Kind = InstrReturnCallIndirect
		}
		return directInstr, err
	case 0x1c:
		imm.Kind = InstrSelect
		return directInstr, skipResultTypeBytes(r)
	case 0x1f:
		if _, err := decodeBlockType(r); err != nil {
			return directInstr, err
		}
		if err := skipCatchVecBytes(r); err != nil {
			return directInstr, err
		}
		imm.Kind = InstrTryTable
		return directTryTable, nil
	case 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e:
		imm.Kind, imm.TouchesMemory = memOpcodeKind[op], true
		return directInstr, classifyMemArgBytes(r, imm, memarg64)
	case 0x3f, 0x40:
		var (
			idx uint32
			err error
		)
		if multiMemory {
			idx, err = r.u32()
		} else {
			err = readReservedZeroByte(r)
		}
		if err != nil {
			return directInstr, err
		}
		imm.Kind, imm.Index, imm.TouchesMemory = InstrMemorySize, idx, true
		if op == 0x40 {
			imm.Kind = InstrMemoryGrow
		}
		return directInstr, nil
	case 0x41:
		_, err := r.i32()
		imm.Kind = InstrI32Const
		return directInstr, err
	case 0x42:
		_, err := r.i64()
		imm.Kind = InstrI64Const
		return directInstr, err
	case 0x43:
		_, err := r.bytes(4)
		imm.Kind = InstrF32Const
		return directInstr, err
	case 0x44:
		_, err := r.bytes(8)
		imm.Kind = InstrF64Const
		return directInstr, err
	case 0xd0:
		imm.Kind = InstrRefNull
		return directInstr, skipRefHeapTypeBytes(r)
	case 0xd3:
		imm.Kind = InstrRefEq
		return directInstr, nil
	case 0xd4:
		imm.Kind = InstrRefAsNonNull
		return directInstr, nil
	case 0xfb:
		return directInstr, classifyFBBytes(r, imm)
	case 0xfc:
		return directInstr, classifyFCBytes(r, imm)
	case 0xfd:
		return directInstr, classifyFDBytes(r, imm, memarg64)
	case 0xfe:
		return directInstr, classifyFEBytes(r, imm, memarg64)
	default:
		return directInstr, &DecodeError{Code: ErrInvalidInstruction, Offset: r.off() - 1}
	}
}

func oneIndexImmediateKind(op byte) InstrKind {
	switch op {
	case 0x08:
		return InstrThrow
	case 0x0c:
		return InstrBr
	case 0x0d:
		return InstrBrIf
	case 0x10:
		return InstrCall
	case 0x12:
		return InstrReturnCall
	case 0x14:
		return InstrCallRef
	case 0x15:
		return InstrReturnCallRef
	case 0x20:
		return InstrLocalGet
	case 0x21:
		return InstrLocalSet
	case 0x22:
		return InstrLocalTee
	case 0x23:
		return InstrGlobalGet
	case 0x24:
		return InstrGlobalSet
	case 0x25:
		return InstrTableGet
	case 0x26:
		return InstrTableSet
	case 0xd2:
		return InstrRefFunc
	case 0xd5:
		return InstrBrOnNull
	case 0xd6:
		return InstrBrOnNonNull
	default:
		return InstrInvalid
	}
}

func skipResultTypeBytes(r *reader) error {
	n, err := r.u32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < n; i++ {
		if err := skipValTypeBytes(r); err != nil {
			return err
		}
	}
	return nil
}

func skipValTypeBytes(r *reader) error {
	_, err := decodeValType(r)
	return err
}

func skipRefHeapTypeBytes(r *reader) error {
	_, _, err := decodeRefHeapType(r)
	return err
}

func classifyMemArgBytes(r *reader, imm *InstructionImmediate, memarg64 bool) error {
	ma, err := decodeMemArgWithWidth(r, memarg64)
	if ma.Mem != nil {
		imm.HasMemIndex = true
		imm.MemIndex = uint32(*ma.Mem)
	}
	return err
}

func skipCatchVecBytes(r *reader) error {
	n, err := r.u32()
	if err != nil {
		return err
	}
	for i := uint32(0); i < n; i++ {
		kind, err := r.byte()
		if err != nil {
			return err
		}
		switch CatchKind(kind) {
		case CatchTag, CatchRef:
			if _, err := r.u32(); err != nil {
				return err
			}
			if _, err := r.u32(); err != nil {
				return err
			}
		case CatchAll, CatchAllRef:
			if _, err := r.u32(); err != nil {
				return err
			}
		default:
			return &DecodeError{Code: ErrInvalidInstruction, Offset: r.off() - 1}
		}
	}
	return nil
}

func classifyFCBytes(r *reader, imm *InstructionImmediate) error {
	sub, err := r.u32()
	imm.Prefix, imm.Subopcode = 0xfc, sub
	if err != nil {
		return err
	}
	if k, ok := fcNoImm[sub]; ok {
		imm.Kind = k
		return nil
	}
	switch sub {
	case 8, 10, 12, 14:
		imm.Kind = fcIndexedKind(sub)
		if imm.Index, err = r.u32(); err != nil {
			return err
		}
		imm.Index2, err = r.u32()
		imm.TouchesMemory = sub == 8 || sub == 10
		imm.UsesBulkMemory = sub == 10
		return err
	case 9, 13, 15, 16, 17:
		imm.Kind = fcIndexedKind(sub)
		imm.Index, err = r.u32()
		return err
	case 11:
		imm.Kind = InstrMemoryFill
		imm.Index, err = r.u32()
		imm.TouchesMemory = true
		imm.UsesBulkMemory = true
		return err
	default:
		return &DecodeError{Code: ErrInvalidInstruction, Offset: r.off()}
	}
}

func fcIndexedKind(sub uint32) InstrKind {
	switch sub {
	case 8:
		return InstrMemoryInit
	case 9:
		return InstrDataDrop
	case 10:
		return InstrMemoryCopy
	case 12:
		return InstrTableInit
	case 13:
		return InstrElemDrop
	case 14:
		return InstrTableCopy
	case 15:
		return InstrTableGrow
	case 16:
		return InstrTableSize
	case 17:
		return InstrTableFill
	default:
		return InstrInvalid
	}
}

func classifyFBBytes(r *reader, imm *InstructionImmediate) error {
	sub, err := r.u32()
	imm.Prefix, imm.Subopcode = 0xfb, sub
	if err != nil {
		return err
	}
	if k, ok := fbNoImm[sub]; ok {
		imm.Kind = k
		return nil
	}
	switch sub {
	case 0xb0, 0xb1, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7:
		imm.Kind = fbStringArrayKind(sub)
		return nil
	case 0, 1, 6, 7, 11, 12, 13, 14, 16, 32, 33, 34, 0x82:
		imm.Kind = fbOneIndexKind(sub)
		imm.Index, err = r.u32()
		return err
	case 2, 3, 4, 5, 8, 9, 10, 17, 18, 19:
		imm.Kind = fbTwoIndexKind(sub)
		if imm.Index, err = r.u32(); err != nil {
			return err
		}
		imm.Index2, err = r.u32()
		return err
	case 20, 21:
		imm.Kind = InstrRefTest
		return skipHeapTypeBytes(r)
	case 22, 23:
		imm.Kind = InstrRefCast
		return skipRefHeapTypeBytes(r)
	case 24, 25:
		if sub == 24 {
			imm.Kind = InstrBrOnCast
		} else {
			imm.Kind = InstrBrOnCastFail
		}
		if _, err := decodeCastOp(r); err != nil {
			return err
		}
		if imm.Index, err = r.u32(); err != nil {
			return err
		}
		if err := skipHeapTypeBytes(r); err != nil {
			return err
		}
		return skipHeapTypeBytes(r)
	case 35, 36:
		imm.Kind = InstrRefCastDescEq
		return skipRefHeapTypeBytes(r)
	default:
		return &DecodeError{Code: ErrInvalidInstruction, Offset: r.off()}
	}
}

func fbOneIndexKind(sub uint32) InstrKind {
	switch sub {
	case 0:
		return InstrStructNew
	case 1:
		return InstrStructNewDefault
	case 6:
		return InstrArrayNew
	case 7:
		return InstrArrayNewDefault
	case 11:
		return InstrArrayGet
	case 12:
		return InstrArrayGetS
	case 13:
		return InstrArrayGetU
	case 14:
		return InstrArraySet
	case 16:
		return InstrArrayFill
	case 32:
		return InstrStructNewDesc
	case 33:
		return InstrStructNewDefaultDesc
	case 34:
		return InstrRefGetDesc
	case 0x82:
		return InstrStringConst
	default:
		return InstrInvalid
	}
}

func fbTwoIndexKind(sub uint32) InstrKind {
	switch sub {
	case 2:
		return InstrStructGet
	case 3:
		return InstrStructGetS
	case 4:
		return InstrStructGetU
	case 5:
		return InstrStructSet
	case 8:
		return InstrArrayNewFixed
	case 9:
		return InstrArrayNewData
	case 10:
		return InstrArrayNewElem
	case 17:
		return InstrArrayCopy
	case 18:
		return InstrArrayInitData
	case 19:
		return InstrArrayInitElem
	default:
		return InstrInvalid
	}
}

func fbStringArrayKind(sub uint32) InstrKind {
	switch sub {
	case 0xb0:
		return InstrStringNewUtf8Array
	case 0xb1:
		return InstrStringNewWtf16Array
	case 0xb2:
		return InstrStringEncodeUtf8Array
	case 0xb3:
		return InstrStringEncodeWtf16Array
	case 0xb4:
		return InstrStringNewLossyUtf8Array
	case 0xb5:
		return InstrStringNewWtf8Array
	case 0xb6:
		return InstrStringEncodeLossyUtf8Array
	case 0xb7:
		return InstrStringEncodeWtf8Array
	default:
		return InstrInvalid
	}
}

func skipHeapTypeBytes(r *reader) error {
	_, err := decodeHeapType(r)
	return err
}

func classifyFDBytes(r *reader, imm *InstructionImmediate, memarg64 bool) error {
	sub, err := r.u32()
	imm.Prefix, imm.Subopcode = 0xfd, sub
	if err != nil {
		return err
	}
	if sub == 12 || sub == 13 {
		_, err := r.bytes(16)
		if err != nil {
			return err
		}
		if sub == 13 {
			start := r.pos - 16
			for i, b := range r.data[start:r.pos] {
				if b >= 32 {
					return &DecodeError{Code: ErrInvalidInstruction, Offset: start + i}
				}
			}
		}
		return nil
	}
	if k, ok := fdNoImm[sub]; ok {
		imm.Kind = k
		return nil
	}
	if k, ok := fdMem[sub]; ok {
		imm.Kind = k
		imm.TouchesMemory = true
		if err = classifyMemArgBytes(r, imm, memarg64); err != nil {
			return err
		}
		if sub >= 84 && sub <= 91 {
			_, err := r.byte()
			return err
		}
		return nil
	}
	if k, ok := fdLane[sub]; ok {
		imm.Kind = k
		_, err := r.byte()
		return err
	}
	return &DecodeError{Code: ErrInvalidInstruction, Offset: r.off()}
}

func classifyFEBytes(r *reader, imm *InstructionImmediate, memarg64 bool) error {
	sub, err := r.u32()
	imm.Prefix, imm.Subopcode = 0xfe, sub
	if err != nil {
		return err
	}
	if sub == 0x03 {
		b, err := r.byte()
		if err != nil {
			return err
		}
		if b != 0 {
			return &DecodeError{Code: ErrInvalidInstruction, Offset: r.off() - 1}
		}
		imm.Kind = InstrAtomicFence
		return nil
	}
	if sub >= 0x5c && sub <= 0x5e {
		if _, err := decodeAtomicOrder(r); err != nil {
			return err
		}
		if imm.Index, err = r.u32(); err != nil {
			return err
		}
		imm.Index2, err = r.u32()
		return err
	}
	if k, ok := feMem[sub]; ok {
		imm.Kind = k
		imm.TouchesMemory = true
		return classifyMemArgBytes(r, imm, memarg64)
	}
	if sub >= 30 && sub <= 71 {
		imm.Kind = InstrAtomicRmw
		imm.TouchesMemory = true
		return classifyMemArgBytes(r, imm, memarg64)
	}
	if sub >= 72 && sub <= 78 {
		imm.Kind = InstrAtomicCmpxchg
		imm.TouchesMemory = true
		return classifyMemArgBytes(r, imm, memarg64)
	}
	return &DecodeError{Code: ErrInvalidInstruction, Offset: r.off()}
}
