// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

func (v *funcValidator) stepBulkMemoryInstruction(in *Instruction) error {
	switch in.Kind {
	case InstrMemoryInit:
		if err := v.checkDataIndex(in.Index, "memory.init"); err != nil {
			return err
		}
		addr, err := v.checkMemArg(MemArg{Mem: ptr(MemIdx(in.Index2))}, 0)
		if err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil { // length in data segment bytes
			return err
		}
		if err := v.popExpect(I32); err != nil { // source offset in data segment
			return err
		}
		return v.popExpect(addr) // destination
	case InstrMemoryCopy:
		addrDst, err := v.checkMemArg(MemArg{Mem: ptr(MemIdx(in.Index))}, 0)
		if err != nil {
			return err
		}
		addrSrc, err := v.checkMemArg(MemArg{Mem: ptr(MemIdx(in.Index2))}, 0)
		if err != nil {
			return err
		}
		if err := v.popExpect(minAddrType(addrDst, addrSrc)); err != nil { // length
			return err
		}
		if err := v.popExpect(addrSrc); err != nil {
			return err
		}
		return v.popExpect(addrDst)
	case InstrMemoryFill:
		addr, err := v.checkMemArg(MemArg{Mem: ptr(MemIdx(in.Index))}, 0)
		if err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil { // length
			return err
		}
		if err := v.popExpect(I32); err != nil { // byte value
			return err
		}
		return v.popExpect(addr) // destination
	case InstrDataDrop:
		return v.checkDataIndex(in.Index, "data.drop")
	}
	return nil
}

func (v *funcValidator) stepBulkTableInstruction(in *Instruction) error {
	switch in.Kind {
	case InstrTableInit:
		// Element expressions were validated during the serial module phase. The
		// body check needs only their immutable declared/result reference type.
		elemRef, err := v.elemRefType(in.Index)
		if err != nil {
			return err
		}
		tt, ok := v.tableType(in.Index2)
		if !ok {
			return v.verr(ErrUnknownTable, "table.init table")
		}
		if !v.refSubtype(elemRef, tt.Ref) {
			return v.verr(ErrTypeMismatch, "table.init element type")
		}
		addr := tableAddrType(tt)
		if err := v.popExpect(I32); err != nil { // length in element-segment entries
			return err
		}
		if err := v.popExpect(I32); err != nil { // source offset in element segment
			return err
		}
		return v.popExpect(addr) // destination table offset
	case InstrTableCopy:
		addrDst, dst, err := v.tableAddrType(in.Index)
		if err != nil {
			return v.verr(ErrUnknownTable, "table.copy dst")
		}
		addrSrc, src, err := v.tableAddrType(in.Index2)
		if err != nil {
			return v.verr(ErrUnknownTable, "table.copy src")
		}
		if !v.refSubtype(src.Ref, dst.Ref) {
			return v.verr(ErrTypeMismatch, "table.copy element type")
		}
		if err := v.popExpect(minAddrType(addrDst, addrSrc)); err != nil {
			return err
		}
		if err := v.popExpect(addrSrc); err != nil {
			return err
		}
		return v.popExpect(addrDst)
	case InstrElemDrop:
		if v.direct != nil {
			if uint(in.Index) >= uint(len(v.direct.elements)) {
				return v.verr(ErrUnknownTable, "elem.drop")
			}
		} else if uint(in.Index) >= uint(len(v.m.Elements)) {
			return v.verr(ErrUnknownTable, "elem.drop")
		}
	}
	return nil
}

func minAddrType(a, b ValType) ValType {
	if equalValType(a, I32) || equalValType(b, I32) {
		return I32
	}
	return I64
}
