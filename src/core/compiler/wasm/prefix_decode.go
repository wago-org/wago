// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

import "github.com/wago-org/wago/internal/buildfeatures"

func lookupPrefixKind(table []InstrKind, sub uint32) (InstrKind, bool) {
	if sub >= uint32(len(table)) {
		return InstrInvalid, false
	}
	kind := table[sub]
	return kind, kind != InstrInvalid
}

func decodeFC(r *reader) (Instruction, error) { return decodeFCWithMultiMemory(r, true) }

func readMemoryIndex(r *reader, multiMemory bool) (uint32, error) {
	if buildfeatures.MultiMemory != 0 && multiMemory {
		return r.u32()
	}
	return 0, readReservedZeroByte(r)
}

func decodeFCWithMultiMemory(r *reader, multiMemory bool) (Instruction, error) {
	sub, err := readFCSubopcode(r)
	if err != nil {
		return Instruction{}, err
	}
	if k, ok := lookupFCNoImmediate(sub); ok {
		return Instruction{Kind: k}, nil
	}
	switch sub {
	case 8:
		if buildfeatures.BulkMemoryOperations == 0 {
			break
		}
		a, err := r.u32()
		if err != nil {
			return Instruction{}, err
		}
		b, err := readMemoryIndex(r, multiMemory)
		return Instruction{Kind: InstrMemoryInit, Index: a, Index2: b}, err
	case 9:
		if buildfeatures.BulkMemoryOperations == 0 {
			break
		}
		return indexInst(r, InstrDataDrop)
	case 10:
		if buildfeatures.BulkMemoryOperations == 0 {
			break
		}
		a, err := readMemoryIndex(r, multiMemory)
		if err != nil {
			return Instruction{}, err
		}
		b, err := readMemoryIndex(r, multiMemory)
		return Instruction{Kind: InstrMemoryCopy, Index: a, Index2: b}, err
	case 11:
		if buildfeatures.BulkMemoryOperations == 0 {
			break
		}
		mi, err := readMemoryIndex(r, multiMemory)
		return Instruction{Kind: InstrMemoryFill, Index: mi}, err
	case 12:
		if buildfeatures.BulkMemoryOperations == 0 || buildfeatures.ReferenceTypes == 0 {
			break
		}
		return twoIndexInst(r, InstrTableInit)
	case 13:
		if buildfeatures.BulkMemoryOperations == 0 || buildfeatures.ReferenceTypes == 0 {
			break
		}
		return indexInst(r, InstrElemDrop)
	case 14:
		if buildfeatures.BulkMemoryOperations == 0 || buildfeatures.ReferenceTypes == 0 {
			break
		}
		return twoIndexInst(r, InstrTableCopy)
	case 15:
		if buildfeatures.ReferenceTypes == 0 {
			break
		}
		return indexInst(r, InstrTableGrow)
	case 16:
		if buildfeatures.ReferenceTypes == 0 {
			break
		}
		return indexInst(r, InstrTableSize)
	case 17:
		if buildfeatures.ReferenceTypes == 0 {
			break
		}
		return indexInst(r, InstrTableFill)
	}
	return Instruction{}, &DecodeError{Code: ErrInvalidInstruction, Offset: r.off()}
}

func readFCSubopcode(r *reader) (uint32, error) {
	sub, err := r.u32()
	if err != nil {
		return sub, err
	}
	switch {
	case buildfeatures.NonTrappingFloatToIntConversion == 0 && sub <= 7:
		return sub, disabledInstruction("nontrapping-float-to-int-conversion", r.off())
	case buildfeatures.BulkMemoryOperations == 0 && sub >= 8 && sub <= 14:
		return sub, disabledInstruction("bulk-memory-operations", r.off())
	case buildfeatures.ReferenceTypes == 0 && sub >= 12 && sub <= 17:
		return sub, disabledInstruction("reference-types", r.off())
	case buildfeatures.WideArithmetic == 0 && sub >= 19 && sub <= 22:
		return sub, disabledInstruction("wide-arithmetic", r.off())
	}
	return sub, nil
}

var fcSaturating = [...]InstrKind{
	InstrI32TruncSatF32S, InstrI32TruncSatF32U, InstrI32TruncSatF64S, InstrI32TruncSatF64U,
	InstrI64TruncSatF32S, InstrI64TruncSatF32U, InstrI64TruncSatF64S, InstrI64TruncSatF64U,
}

var fcWide = [...]InstrKind{InstrI64Add128, InstrI64Sub128, InstrI64MulWideS, InstrI64MulWideU}

func lookupFCNoImmediate(sub uint32) (InstrKind, bool) {
	if buildfeatures.NonTrappingFloatToIntConversion != 0 && sub < uint32(len(fcSaturating)) {
		return fcSaturating[sub], true
	}
	if buildfeatures.WideArithmetic != 0 {
		return lookupPrefixKind(fcWide[:], sub-19)
	}
	return InstrInvalid, false
}
