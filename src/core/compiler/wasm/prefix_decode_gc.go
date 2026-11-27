// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

func decodeFB(r *reader) (Instruction, error) { return decodeFBInto(r, nil) }

// decodeFBInto can reuse validator storage. The caller must consume the
// instruction before decoding another opcode into ext. Tree decoders pass nil
// so each retained instruction owns its immediate.
func decodeFBInto(r *reader, ext *instrExt) (Instruction, error) {
	sub, err := r.u32()
	if err != nil {
		return Instruction{}, err
	}
	if k, ok := lookupPrefixKind(fbNoImm[:], sub); ok {
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
		if ext == nil {
			ext = &instrExt{}
		}
		*ext = instrExt{HeapType: ht}
		return Instruction{Kind: InstrRefTest, Cast: CastOp{TargetNullable: sub == 21}, ext: ext}, err
	case 22, 23:
		exact, ht, err := decodeRefHeapType(r)
		if ext == nil {
			ext = &instrExt{}
		}
		*ext = instrExt{HeapType: ht}
		return Instruction{Kind: InstrRefCast, Cast: CastOp{TargetNullable: sub == 23, SourceNullable: exact}, ext: ext}, err
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
		if ext == nil {
			ext = &instrExt{}
		}
		*ext = instrExt{HeapType: ht1, HeapType2: ht2}
		return Instruction{Kind: k, Index: l, Cast: cast, ext: ext}, nil
	case 34:
		idx, err := r.u32()
		return Instruction{Kind: InstrRefGetDesc, Index: idx}, err
	case 35, 36:
		exact, ht, err := decodeRefHeapType(r)
		if ext == nil {
			ext = &instrExt{}
		}
		*ext = instrExt{HeapType: ht}
		return Instruction{Kind: InstrRefCastDescEq, Cast: CastOp{TargetNullable: sub == 36, SourceNullable: exact}, ext: ext}, err
	default:
		return Instruction{}, &DecodeError{Code: ErrInvalidInstruction, Offset: r.off()}
	}
}

var fbNoImm = [...]InstrKind{15: InstrArrayLen, 26: InstrAnyConvertExtern, 27: InstrExternConvertAny, 28: InstrRefI31, 29: InstrI31GetS, 30: InstrI31GetU}

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
