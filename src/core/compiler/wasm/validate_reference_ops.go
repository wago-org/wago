// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

func (v *funcValidator) stepReferenceInstruction(in *Instruction) error {
	switch in.Kind {
	case InstrTableGet:
		addr, tt, err := v.tableAddrType(in.Index)
		if err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(RefVal(tt.Ref))
	case InstrTableSet:
		addr, tt, err := v.tableAddrType(in.Index)
		if err != nil {
			return err
		}
		if err := v.popExpect(RefVal(tt.Ref)); err != nil {
			return err
		}
		return v.popExpect(addr)
	case InstrRefNull:
		if err := v.validateRefType(in.RefType()); err != nil {
			return err
		}
		v.push(RefVal(in.RefType()))
	case InstrRefFunc:
		typeIdx, ok := v.m.FuncTypeIndex(in.Index)
		if !ok {
			return v.verr(ErrUnknownFunc, "ref.func")
		}
		if !v.isDeclaredFunc(in.Index) {
			return v.verr(ErrUnknownFunc, "undeclared function reference")
		}
		// Core 3.0 gives ref.func the exact declared function type rather than
		// collapsing it to the abstract funcref supertype. The reference is
		// non-null and remains a subtype of funcref for Release 2 consumers.
		v.push(RefVal(Ref(false, IndexedHeap(typeIdx), false)))
	case InstrRefIsNull:
		x, err := v.pop()
		if err != nil {
			return err
		}
		if !x.unknown() && x.t.Kind() != ValRef {
			return v.verr(ErrTypeMismatch, "ref.is_null")
		}
		v.push(I32)
	}
	return nil
}

func (v *funcValidator) stepTypedReferenceInstruction(in *Instruction) error {
	switch in.Kind {
	case InstrRefAsNonNull:
		x, err := v.pop()
		if err != nil {
			return err
		}
		if !x.unknown() && x.t.Kind() != ValRef {
			return v.verr(ErrTypeMismatch, "ref.as_non_null")
		}
		v.push(nonNullValidationType(x))
	case InstrBrOnNull:
		lt, err := v.label(in.Index)
		if err != nil {
			return err
		}
		x, err := v.pop()
		if err != nil {
			return err
		}
		if !x.unknown() && x.t.Kind() != ValRef {
			return v.verr(ErrTypeMismatch, "br_on_null")
		}
		if err := v.popAll(lt); err != nil {
			return err
		}
		v.pushAll(lt)
		v.push(nonNullValidationType(x))
	case InstrBrOnNonNull:
		lt, err := v.label(in.Index)
		if err != nil {
			return err
		}
		x, err := v.pop()
		if err != nil {
			return err
		}
		if len(lt) == 0 || lt[len(lt)-1].Kind() != ValRef || (!x.unknown() && x.t.Kind() != ValRef) {
			return v.verr(ErrTypeMismatch, "br_on_non_null")
		}
		if !x.unknown() {
			x.t = RefVal(x.t.Ref().WithNullable(false))
			if !v.subtype(x.t, lt[len(lt)-1]) {
				return v.verr(ErrTypeMismatch, x.t.String()+" is not "+lt[len(lt)-1].String())
			}
		}
		prefix := lt[:len(lt)-1]
		if err := v.popAll(prefix); err != nil {
			return err
		}
		// A taken branch appends the non-null reference to the label payload.
		// The null fallthrough consumes the reference and retains only the
		// payload values that precede it.
		v.pushAll(prefix)
	}
	return nil
}

func (v *funcValidator) stepReferenceTableInstruction(in *Instruction) error {
	switch in.Kind {
	case InstrTableSize:
		addr, _, err := v.tableAddrType(in.Index)
		if err != nil {
			return v.verr(ErrUnknownTable, "table.size")
		}
		v.push(addr)
	case InstrTableGrow:
		addr, tt, err := v.tableAddrType(in.Index)
		if err != nil {
			return v.verr(ErrUnknownTable, "table.grow")
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		if err := v.popExpect(RefVal(tt.Ref)); err != nil {
			return err
		}
		v.push(addr)
	case InstrTableFill:
		addr, tt, err := v.tableAddrType(in.Index)
		if err != nil {
			return v.verr(ErrUnknownTable, "table.fill")
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		if err := v.popExpect(RefVal(tt.Ref)); err != nil {
			return err
		}
		return v.popExpect(addr)
	}
	return nil
}
