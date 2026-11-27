// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

func decodeFEWithMemarg64(r *reader, memarg64 bool) (Instruction, error) {
	return decodeFEWithMemargWidths(r, fixedMemargWidths(memarg64))
}

func decodeFEWithMemargWidths(r *reader, widths memargWidths) (Instruction, error) {
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
	if k, ok := lookupPrefixKind(feMem[:], sub); ok {
		ma, err := decodeMemArgWithWidths(r, widths)
		return Instruction{Kind: k, AtomicOp: sub, ext: &instrExt{MemArg: ma}}, err
	}
	if sub >= 30 && sub <= 71 {
		ma, err := decodeMemArgWithWidths(r, widths)
		return Instruction{Kind: InstrAtomicRmw, AtomicOp: sub, ext: &instrExt{MemArg: ma}}, err
	}
	if sub >= 72 && sub <= 78 {
		ma, err := decodeMemArgWithWidths(r, widths)
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

var feMem = [...]InstrKind{0x00: InstrMemoryAtomicNotify, 0x01: InstrMemoryAtomicWait32, 0x02: InstrMemoryAtomicWait64, 0x10: InstrI32AtomicLoad, 0x11: InstrI64AtomicLoad, 0x12: InstrI32AtomicLoad8U, 0x13: InstrI32AtomicLoad16U, 0x14: InstrI64AtomicLoad8U, 0x15: InstrI64AtomicLoad16U, 0x16: InstrI64AtomicLoad32U, 0x17: InstrI32AtomicStore, 0x18: InstrI64AtomicStore, 0x19: InstrI32AtomicStore8, 0x1a: InstrI32AtomicStore16, 0x1b: InstrI64AtomicStore8, 0x1c: InstrI64AtomicStore16, 0x1d: InstrI64AtomicStore32}
