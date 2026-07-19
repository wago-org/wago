package wasm

func decodeFC(r *reader) (Instruction, error) {
	sub, err := r.u32()
	if err != nil {
		return Instruction{}, err
	}
	if k, ok := fcNoImm[sub]; ok {
		return Instruction{Kind: k}, nil
	}
	switch sub {
	case 8:
		return twoIndexInst(r, InstrMemoryInit)
	case 9:
		return indexInst(r, InstrDataDrop)
	case 10:
		return twoIndexInst(r, InstrMemoryCopy)
	case 11:
		mi, err := r.u32()
		return Instruction{Kind: InstrMemoryFill, Index: mi}, err
	case 12:
		return twoIndexInst(r, InstrTableInit)
	case 13:
		return indexInst(r, InstrElemDrop)
	case 14:
		return twoIndexInst(r, InstrTableCopy)
	case 15:
		return indexInst(r, InstrTableGrow)
	case 16:
		return indexInst(r, InstrTableSize)
	case 17:
		return indexInst(r, InstrTableFill)
	default:
		return Instruction{}, &DecodeError{Code: ErrInvalidInstruction, Offset: r.off()}
	}
}

var fcNoImm = map[uint32]InstrKind{0: InstrI32TruncSatF32S, 1: InstrI32TruncSatF32U, 2: InstrI32TruncSatF64S, 3: InstrI32TruncSatF64U, 4: InstrI64TruncSatF32S, 5: InstrI64TruncSatF32U, 6: InstrI64TruncSatF64S, 7: InstrI64TruncSatF64U}

func decodeFB(r *reader) (Instruction, error) {
	sub, err := r.u32()
	if err != nil {
		return Instruction{}, err
	}
	if k, ok := fbNoImm[sub]; ok {
		return Instruction{Kind: k}, nil
	}
	switch sub {
	case 0:
		idx, err := r.u32()
		return Instruction{Kind: InstrStructNew, Index: idx}, err
	case 1:
		idx, err := r.u32()
		return Instruction{Kind: InstrStructNewDefault, Index: idx}, err
	case 32:
		idx, err := r.u32()
		return Instruction{Kind: InstrStructNewDesc, Index: idx}, err
	case 33:
		idx, err := r.u32()
		return Instruction{Kind: InstrStructNewDefaultDesc, Index: idx}, err
	case 2:
		return twoIndexInst(r, InstrStructGet)
	case 3:
		return twoIndexInst(r, InstrStructGetS)
	case 4:
		return twoIndexInst(r, InstrStructGetU)
	case 5:
		return twoIndexInst(r, InstrStructSet)
	case 6:
		idx, err := r.u32()
		return Instruction{Kind: InstrArrayNew, Index: idx}, err
	case 7:
		idx, err := r.u32()
		return Instruction{Kind: InstrArrayNewDefault, Index: idx}, err
	case 8:
		return twoIndexInst(r, InstrArrayNewFixed)
	case 9:
		return twoIndexInst(r, InstrArrayNewData)
	case 10:
		return twoIndexInst(r, InstrArrayNewElem)
	case 11:
		idx, err := r.u32()
		return Instruction{Kind: InstrArrayGet, Index: idx}, err
	case 12:
		idx, err := r.u32()
		return Instruction{Kind: InstrArrayGetS, Index: idx}, err
	case 13:
		idx, err := r.u32()
		return Instruction{Kind: InstrArrayGetU, Index: idx}, err
	case 14:
		idx, err := r.u32()
		return Instruction{Kind: InstrArraySet, Index: idx}, err
	case 16:
		idx, err := r.u32()
		return Instruction{Kind: InstrArrayFill, Index: idx}, err
	case 17:
		return twoIndexInst(r, InstrArrayCopy)
	case 18:
		return twoIndexInst(r, InstrArrayInitData)
	case 19:
		return twoIndexInst(r, InstrArrayInitElem)
	case 0x82:
		idx, err := r.u32()
		return Instruction{Kind: InstrStringConst, Index: idx}, err
	case 0xb0:
		return Instruction{Kind: InstrStringNewUtf8Array}, nil
	case 0xb1:
		return Instruction{Kind: InstrStringNewWtf16Array}, nil
	case 0xb2:
		return Instruction{Kind: InstrStringEncodeUtf8Array}, nil
	case 0xb3:
		return Instruction{Kind: InstrStringEncodeWtf16Array}, nil
	case 0xb4:
		return Instruction{Kind: InstrStringNewLossyUtf8Array}, nil
	case 0xb5:
		return Instruction{Kind: InstrStringNewWtf8Array}, nil
	case 0xb6:
		return Instruction{Kind: InstrStringEncodeLossyUtf8Array}, nil
	case 0xb7:
		return Instruction{Kind: InstrStringEncodeWtf8Array}, nil
	case 20, 21:
		ht, err := decodeHeapType(r)
		return Instruction{Kind: InstrRefTest, Cast: CastOp{TargetNullable: sub == 21}, ext: &instrExt{HeapType: ht}}, err
	case 22, 23:
		exact, ht, err := decodeRefHeapType(r)
		return Instruction{Kind: InstrRefCast, Cast: CastOp{TargetNullable: sub == 23, SourceNullable: exact}, ext: &instrExt{HeapType: ht}}, err
	case 24, 25:
		cast, err := decodeCastOp(r)
		if err != nil {
			return Instruction{}, err
		}
		l, err := r.u32()
		if err != nil {
			return Instruction{}, err
		}
		ht1, err := decodeHeapType(r)
		if err != nil {
			return Instruction{}, err
		}
		ht2, err := decodeHeapType(r)
		if err != nil {
			return Instruction{}, err
		}
		k := InstrBrOnCast
		if sub == 25 {
			k = InstrBrOnCastFail
		}
		return Instruction{Kind: k, Index: l, Cast: cast, ext: &instrExt{HeapType: ht1, HeapType2: ht2}}, nil
	case 34:
		idx, err := r.u32()
		return Instruction{Kind: InstrRefGetDesc, Index: idx}, err
	case 35, 36:
		exact, ht, err := decodeRefHeapType(r)
		return Instruction{Kind: InstrRefCastDescEq, Cast: CastOp{TargetNullable: sub == 36, SourceNullable: exact}, ext: &instrExt{HeapType: ht}}, err
	default:
		return Instruction{}, &DecodeError{Code: ErrInvalidInstruction, Offset: r.off()}
	}
}

var fbNoImm = map[uint32]InstrKind{15: InstrArrayLen, 26: InstrAnyConvertExtern, 27: InstrExternConvertAny, 28: InstrRefI31, 29: InstrI31GetS, 30: InstrI31GetU}

func decodeCastOp(r *reader) (CastOp, error) {
	b, err := r.byte()
	if err != nil {
		return CastOp{}, err
	}
	switch b {
	case 0:
		return CastOp{}, nil
	case 1:
		return CastOp{SourceNullable: true}, nil
	case 2:
		return CastOp{TargetNullable: true}, nil
	case 3:
		return CastOp{SourceNullable: true, TargetNullable: true}, nil
	default:
		return CastOp{}, &DecodeError{Code: ErrInvalidInstruction, Offset: r.off() - 1}
	}
}

func decodeFEWithMemarg64(r *reader, memarg64 bool) (Instruction, error) {
	sub, err := r.u32()
	if err != nil {
		return Instruction{}, err
	}
	if sub == 0x03 {
		b, err := r.byte()
		if err != nil {
			return Instruction{}, err
		}
		if b != 0 {
			return Instruction{}, &DecodeError{Code: ErrInvalidInstruction, Offset: r.off() - 1}
		}
		return Instruction{Kind: InstrAtomicFence}, nil
	}
	if sub >= 0x5c && sub <= 0x5e {
		order, err := decodeAtomicOrder(r)
		if err != nil {
			return Instruction{}, err
		}
		typeIdx, err := r.u32()
		if err != nil {
			return Instruction{}, err
		}
		fieldIdx, err := r.u32()
		if err != nil {
			return Instruction{}, err
		}
		k := InstrStructAtomicGet
		if sub == 0x5d {
			k = InstrStructAtomicGetS
		} else if sub == 0x5e {
			k = InstrStructAtomicGetU
		}
		return Instruction{Kind: k, AtomicOrder: order, Index: typeIdx, Index2: fieldIdx}, nil
	}
	if k, ok := feMem[sub]; ok {
		ma, err := decodeMemArgWithWidth(r, memarg64)
		return Instruction{Kind: k, AtomicOp: sub, ext: &instrExt{MemArg: ma}}, err
	}
	if sub >= 30 && sub <= 71 {
		ma, err := decodeMemArgWithWidth(r, memarg64)
		return Instruction{Kind: InstrAtomicRmw, AtomicOp: sub, ext: &instrExt{MemArg: ma}}, err
	}
	if sub >= 72 && sub <= 78 {
		ma, err := decodeMemArgWithWidth(r, memarg64)
		return Instruction{Kind: InstrAtomicCmpxchg, AtomicOp: sub, ext: &instrExt{MemArg: ma}}, err
	}
	return Instruction{}, &DecodeError{Code: ErrInvalidInstruction, Offset: r.off()}
}

func decodeAtomicOrder(r *reader) (AtomicOrder, error) {
	b, err := r.byte()
	if err != nil {
		return 0, err
	}
	if b == byte(SeqCst) || b == byte(AcqRel) {
		return AtomicOrder(b), nil
	}
	return 0, &DecodeError{Code: ErrInvalidInstruction, Offset: r.off() - 1}
}

var feMem = map[uint32]InstrKind{0x00: InstrMemoryAtomicNotify, 0x01: InstrMemoryAtomicWait32, 0x02: InstrMemoryAtomicWait64, 0x10: InstrI32AtomicLoad, 0x11: InstrI64AtomicLoad, 0x12: InstrI32AtomicLoad8U, 0x13: InstrI32AtomicLoad16U, 0x14: InstrI64AtomicLoad8U, 0x15: InstrI64AtomicLoad16U, 0x16: InstrI64AtomicLoad32U, 0x17: InstrI32AtomicStore, 0x18: InstrI64AtomicStore, 0x19: InstrI32AtomicStore8, 0x1a: InstrI32AtomicStore16, 0x1b: InstrI64AtomicStore8, 0x1c: InstrI64AtomicStore16, 0x1d: InstrI64AtomicStore32}

func decodeFDWithMemarg64(r *reader, memarg64 bool) (Instruction, error) {
	sub, err := r.u32()
	if err != nil {
		return Instruction{}, err
	}
	if sub == 12 {
		var bs [16]LaneIdx
		for i := range bs {
			b, err := r.byte()
			if err != nil {
				return Instruction{}, err
			}
			bs[i] = LaneIdx(b)
		}
		return Instruction{Kind: InstrV128Const, ext: &instrExt{Lanes: bs}}, nil
	}
	if sub == 13 {
		var lanes [16]LaneIdx
		for i := range lanes {
			b, err := r.byte()
			if err != nil {
				return Instruction{}, err
			}
			if b >= 32 {
				return Instruction{}, &DecodeError{Code: ErrInvalidInstruction, Offset: r.off() - 1}
			}
			lanes[i] = LaneIdx(b)
		}
		return Instruction{Kind: InstrI8x16Shuffle, ext: &instrExt{Lanes: lanes}}, nil
	}
	if k, ok := fdNoImm[sub]; ok {
		return Instruction{Kind: k}, nil
	}
	if k, ok := fdMem[sub]; ok {
		ma, err := decodeMemArgWithWidth(r, memarg64)
		if err != nil {
			return Instruction{}, err
		}
		in := Instruction{Kind: k, ext: &instrExt{MemArg: ma}}
		if sub >= 84 && sub <= 91 {
			// SIMD lane memory instructions carry a lane immediate after memarg.
			lane, err := r.byte()
			if err != nil {
				return Instruction{}, err
			}
			in.Lane = LaneIdx(lane)
		}
		return in, nil
	}
	if k, ok := fdLane[sub]; ok {
		lane, err := r.byte()
		return Instruction{Kind: k, Lane: LaneIdx(lane)}, err
	}
	return Instruction{}, &DecodeError{Code: ErrInvalidInstruction, Offset: r.off()}
}

var fdMem = map[uint32]InstrKind{0: InstrV128Load, 1: InstrV128Load8x8S, 2: InstrV128Load8x8U, 3: InstrV128Load16x4S, 4: InstrV128Load16x4U, 5: InstrV128Load32x2S, 6: InstrV128Load32x2U, 7: InstrV128Load8Splat, 8: InstrV128Load16Splat, 9: InstrV128Load32Splat, 10: InstrV128Load64Splat, 11: InstrV128Store, 84: InstrV128Load8Lane, 85: InstrV128Load16Lane, 86: InstrV128Load32Lane, 87: InstrV128Load64Lane, 88: InstrV128Store8Lane, 89: InstrV128Store16Lane, 90: InstrV128Store32Lane, 91: InstrV128Store64Lane, 92: InstrV128Load32Zero, 93: InstrV128Load64Zero}
var fdLane = map[uint32]InstrKind{21: InstrI8x16ExtractLaneS, 22: InstrI8x16ExtractLaneU, 23: InstrI8x16ReplaceLane, 24: InstrI16x8ExtractLaneS, 25: InstrI16x8ExtractLaneU, 26: InstrI16x8ReplaceLane, 27: InstrI32x4ExtractLane, 28: InstrI32x4ReplaceLane, 29: InstrI64x2ExtractLane, 30: InstrI64x2ReplaceLane, 31: InstrF32x4ExtractLane, 32: InstrF32x4ReplaceLane, 33: InstrF64x2ExtractLane, 34: InstrF64x2ReplaceLane}

// SIMDSubopcodeValid reports whether sub is one of the core SIMD or relaxed-SIMD
// instructions accepted by the 0xfd decoder. It intentionally excludes the 20
// reserved holes in the proposal table.
// SIMDNoImmediateSignature returns the validated operand and result types for
// a SIMD subopcode that carries no immediate bytes after its subopcode. The
// slices are newly allocated so callers cannot mutate validator state.
func SIMDNoImmediateSignature(sub uint32) (inputs, results []ValType, ok bool) {
	kind, ok := fdNoImm[sub]
	if !ok {
		return nil, nil, false
	}
	effect := simdEffects[kind]
	switch effect.cat {
	case simdEffSplat:
		return []ValType{effect.scalar}, []ValType{V128}, true
	case simdEffShift:
		return []ValType{V128, I32}, []ValType{V128}, true
	case simdEffUnary:
		return []ValType{V128}, []ValType{V128}, true
	case simdEffBinary:
		return []ValType{V128, V128}, []ValType{V128}, true
	case simdEffTernary, simdBitselect:
		return []ValType{V128, V128, V128}, []ValType{V128}, true
	case simdPopV128PushI32:
		return []ValType{V128}, []ValType{I32}, true
	default:
		return nil, nil, false
	}
}

func SIMDSubopcodeValid(sub uint32) bool {
	if sub == 12 || sub == 13 {
		return true
	}
	if _, ok := fdNoImm[sub]; ok {
		return true
	}
	if _, ok := fdMem[sub]; ok {
		return true
	}
	_, ok := fdLane[sub]
	return ok
}

var fdNoImm = map[uint32]InstrKind{
	14:  InstrI8x16Swizzle,
	15:  InstrI8x16Splat,
	16:  InstrI16x8Splat,
	17:  InstrI32x4Splat,
	18:  InstrI64x2Splat,
	19:  InstrF32x4Splat,
	20:  InstrF64x2Splat,
	35:  InstrI8x16Eq,
	36:  InstrI8x16Ne,
	37:  InstrI8x16LtS,
	38:  InstrI8x16LtU,
	39:  InstrI8x16GtS,
	40:  InstrI8x16GtU,
	41:  InstrI8x16LeS,
	42:  InstrI8x16LeU,
	43:  InstrI8x16GeS,
	44:  InstrI8x16GeU,
	45:  InstrI16x8Eq,
	46:  InstrI16x8Ne,
	47:  InstrI16x8LtS,
	48:  InstrI16x8LtU,
	49:  InstrI16x8GtS,
	50:  InstrI16x8GtU,
	51:  InstrI16x8LeS,
	52:  InstrI16x8LeU,
	53:  InstrI16x8GeS,
	54:  InstrI16x8GeU,
	55:  InstrI32x4Eq,
	56:  InstrI32x4Ne,
	57:  InstrI32x4LtS,
	58:  InstrI32x4LtU,
	59:  InstrI32x4GtS,
	60:  InstrI32x4GtU,
	61:  InstrI32x4LeS,
	62:  InstrI32x4LeU,
	63:  InstrI32x4GeS,
	64:  InstrI32x4GeU,
	65:  InstrF32x4Eq,
	66:  InstrF32x4Ne,
	67:  InstrF32x4Lt,
	68:  InstrF32x4Gt,
	69:  InstrF32x4Le,
	70:  InstrF32x4Ge,
	71:  InstrF64x2Eq,
	72:  InstrF64x2Ne,
	73:  InstrF64x2Lt,
	74:  InstrF64x2Gt,
	75:  InstrF64x2Le,
	76:  InstrF64x2Ge,
	77:  InstrV128Not,
	78:  InstrV128And,
	79:  InstrV128Andnot,
	80:  InstrV128Or,
	81:  InstrV128Xor,
	82:  InstrV128Bitselect,
	83:  InstrV128AnyTrue,
	94:  InstrF32x4DemoteF64x2Zero,
	95:  InstrF64x2PromoteLowF32x4,
	96:  InstrI8x16Abs,
	97:  InstrI8x16Neg,
	98:  InstrI8x16Popcnt,
	99:  InstrI8x16AllTrue,
	100: InstrI8x16Bitmask,
	101: InstrI8x16NarrowI16x8S,
	102: InstrI8x16NarrowI16x8U,
	103: InstrF32x4Ceil,
	104: InstrF32x4Floor,
	105: InstrF32x4Trunc,
	106: InstrF32x4Nearest,
	107: InstrI8x16Shl,
	108: InstrI8x16ShrS,
	109: InstrI8x16ShrU,
	110: InstrI8x16Add,
	111: InstrI8x16AddSatS,
	112: InstrI8x16AddSatU,
	113: InstrI8x16Sub,
	114: InstrI8x16SubSatS,
	115: InstrI8x16SubSatU,
	116: InstrF64x2Ceil,
	117: InstrF64x2Floor,
	118: InstrI8x16MinS,
	119: InstrI8x16MinU,
	120: InstrI8x16MaxS,
	121: InstrI8x16MaxU,
	122: InstrF64x2Trunc,
	123: InstrI8x16AvgrU,
	124: InstrI16x8ExtaddPairwiseI8x16S,
	125: InstrI16x8ExtaddPairwiseI8x16U,
	126: InstrI32x4ExtaddPairwiseI16x8S,
	127: InstrI32x4ExtaddPairwiseI16x8U,
	128: InstrI16x8Abs,
	129: InstrI16x8Neg,
	130: InstrI16x8Q15mulrSatS,
	131: InstrI16x8AllTrue,
	132: InstrI16x8Bitmask,
	133: InstrI16x8NarrowI32x4S,
	134: InstrI16x8NarrowI32x4U,
	135: InstrI16x8ExtendLowI8x16S,
	136: InstrI16x8ExtendHighI8x16S,
	137: InstrI16x8ExtendLowI8x16U,
	138: InstrI16x8ExtendHighI8x16U,
	139: InstrI16x8Shl,
	140: InstrI16x8ShrS,
	141: InstrI16x8ShrU,
	142: InstrI16x8Add,
	143: InstrI16x8AddSatS,
	144: InstrI16x8AddSatU,
	145: InstrI16x8Sub,
	146: InstrI16x8SubSatS,
	147: InstrI16x8SubSatU,
	148: InstrF64x2Nearest,
	149: InstrI16x8Mul,
	150: InstrI16x8MinS,
	151: InstrI16x8MinU,
	152: InstrI16x8MaxS,
	153: InstrI16x8MaxU,
	155: InstrI16x8AvgrU,
	156: InstrI16x8ExtmulLowI8x16S,
	157: InstrI16x8ExtmulHighI8x16S,
	158: InstrI16x8ExtmulLowI8x16U,
	159: InstrI16x8ExtmulHighI8x16U,
	160: InstrI32x4Abs,
	161: InstrI32x4Neg,
	163: InstrI32x4AllTrue,
	164: InstrI32x4Bitmask,
	167: InstrI32x4ExtendLowI16x8S,
	168: InstrI32x4ExtendHighI16x8S,
	169: InstrI32x4ExtendLowI16x8U,
	170: InstrI32x4ExtendHighI16x8U,
	171: InstrI32x4Shl,
	172: InstrI32x4ShrS,
	173: InstrI32x4ShrU,
	174: InstrI32x4Add,
	177: InstrI32x4Sub,
	181: InstrI32x4Mul,
	182: InstrI32x4MinS,
	183: InstrI32x4MinU,
	184: InstrI32x4MaxS,
	185: InstrI32x4MaxU,
	186: InstrI32x4DotI16x8S,
	188: InstrI32x4ExtmulLowI16x8S,
	189: InstrI32x4ExtmulHighI16x8S,
	190: InstrI32x4ExtmulLowI16x8U,
	191: InstrI32x4ExtmulHighI16x8U,
	192: InstrI64x2Abs,
	193: InstrI64x2Neg,
	195: InstrI64x2AllTrue,
	196: InstrI64x2Bitmask,
	199: InstrI64x2ExtendLowI32x4S,
	200: InstrI64x2ExtendHighI32x4S,
	201: InstrI64x2ExtendLowI32x4U,
	202: InstrI64x2ExtendHighI32x4U,
	203: InstrI64x2Shl,
	204: InstrI64x2ShrS,
	205: InstrI64x2ShrU,
	206: InstrI64x2Add,
	209: InstrI64x2Sub,
	213: InstrI64x2Mul,
	214: InstrI64x2Eq,
	215: InstrI64x2Ne,
	216: InstrI64x2LtS,
	217: InstrI64x2GtS,
	218: InstrI64x2LeS,
	219: InstrI64x2GeS,
	220: InstrI64x2ExtmulLowI32x4S,
	221: InstrI64x2ExtmulHighI32x4S,
	222: InstrI64x2ExtmulLowI32x4U,
	223: InstrI64x2ExtmulHighI32x4U,
	224: InstrF32x4Abs,
	225: InstrF32x4Neg,
	227: InstrF32x4Sqrt,
	228: InstrF32x4Add,
	229: InstrF32x4Sub,
	230: InstrF32x4Mul,
	231: InstrF32x4Div,
	232: InstrF32x4Min,
	233: InstrF32x4Max,
	234: InstrF32x4Pmin,
	235: InstrF32x4Pmax,
	236: InstrF64x2Abs,
	237: InstrF64x2Neg,
	239: InstrF64x2Sqrt,
	240: InstrF64x2Add,
	241: InstrF64x2Sub,
	242: InstrF64x2Mul,
	243: InstrF64x2Div,
	244: InstrF64x2Min,
	245: InstrF64x2Max,
	246: InstrF64x2Pmin,
	247: InstrF64x2Pmax,
	248: InstrI32x4TruncSatF32x4S,
	249: InstrI32x4TruncSatF32x4U,
	250: InstrF32x4ConvertI32x4S,
	251: InstrF32x4ConvertI32x4U,
	252: InstrI32x4TruncSatF64x2SZero,
	253: InstrI32x4TruncSatF64x2UZero,
	254: InstrF64x2ConvertLowI32x4S,
	255: InstrF64x2ConvertLowI32x4U,
	256: InstrI8x16RelaxedSwizzle,
	257: InstrI32x4RelaxedTruncF32x4S,
	258: InstrI32x4RelaxedTruncF32x4U,
	259: InstrI32x4RelaxedTruncZeroF64x2S,
	260: InstrI32x4RelaxedTruncZeroF64x2U,
	261: InstrF32x4RelaxedMadd,
	262: InstrF32x4RelaxedNmadd,
	263: InstrF64x2RelaxedMadd,
	264: InstrF64x2RelaxedNmadd,
	265: InstrI8x16RelaxedLaneselect,
	266: InstrI16x8RelaxedLaneselect,
	267: InstrI32x4RelaxedLaneselect,
	268: InstrI64x2RelaxedLaneselect,
	269: InstrF32x4RelaxedMin,
	270: InstrF32x4RelaxedMax,
	271: InstrF64x2RelaxedMin,
	272: InstrF64x2RelaxedMax,
	273: InstrI16x8RelaxedQ15mulrS,
	274: InstrI16x8RelaxedDotI8x16I7x16S,
	275: InstrI32x4RelaxedDotI8x16I7x16AddS,
}
