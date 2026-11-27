// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wasm

func (v *funcValidator) stepAtomic(in Instruction) error {
	if in.Kind == InstrAtomicFence {
		return nil
	}
	if in.Kind == InstrMemoryAtomicNotify {
		addr, err := v.checkAtomicMemArg(in.MemArg(), 2)
		if err != nil {
			return err
		}
		if err := v.popExpect(I32); err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(I32)
		return nil
	}
	if in.Kind == InstrMemoryAtomicWait32 || in.Kind == InstrMemoryAtomicWait64 {
		natural := uint32(2)
		if in.Kind == InstrMemoryAtomicWait64 {
			natural = 3
		}
		addr, err := v.checkAtomicMemArg(in.MemArg(), natural)
		if err != nil {
			return err
		}
		if err := v.popExpect(I64); err != nil {
			return err
		}
		want := I32
		if in.Kind == InstrMemoryAtomicWait64 {
			want = I64
		}
		if err := v.popExpect(want); err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(I32)
		return nil
	}
	if eff, ok := lookupAtomicEffect(atomicLoadEffects[:], InstrI32AtomicLoad, in.Kind); ok {
		addr, err := v.checkAtomicMemArg(in.MemArg(), uint32(eff.align))
		if err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(eff.typ.valType())
		return nil
	}
	if eff, ok := lookupAtomicEffect(atomicLoadEffects[:], InstrI32AtomicStore, in.Kind); ok {
		addr, err := v.checkAtomicMemArg(in.MemArg(), uint32(eff.align))
		if err != nil {
			return err
		}
		if err := v.popExpect(eff.typ.valType()); err != nil {
			return err
		}
		return v.popExpect(addr)
	}
	if in.Kind == InstrAtomicRmw {
		eff := atomicRmwEffect(in.AtomicOp)
		typ := eff.typ.valType()
		addr, err := v.checkAtomicMemArg(in.MemArg(), uint32(eff.align))
		if err != nil {
			return err
		}
		if err := v.popExpect(typ); err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(typ)
		return nil
	}
	if in.Kind == InstrAtomicCmpxchg {
		eff := atomicCmpxchgEffect(in.AtomicOp)
		typ := eff.typ.valType()
		addr, err := v.checkAtomicMemArg(in.MemArg(), uint32(eff.align))
		if err != nil {
			return err
		}
		if err := v.popExpect(typ); err != nil {
			return err
		}
		if err := v.popExpect(typ); err != nil {
			return err
		}
		if err := v.popExpect(addr); err != nil {
			return err
		}
		v.push(typ)
		return nil
	}
	return v.verr(ErrUnsupportedValidationOpcode, in.Kind.String())
}

type atomicEffect struct {
	typ   effectValue
	align uint8
}

var atomicLoadEffects = [...]atomicEffect{
	{typ: effectI32, align: 2},
	{typ: effectI64, align: 3},
	{typ: effectI32},
	{typ: effectI32, align: 1},
	{typ: effectI64},
	{typ: effectI64, align: 1},
	{typ: effectI64, align: 2},
}

func lookupAtomicEffect(table []atomicEffect, first, kind InstrKind) (atomicEffect, bool) {
	index := uint32(kind) - uint32(first)
	if index >= uint32(len(table)) {
		return atomicEffect{}, false
	}
	return table[index], true
}

func atomicRmwEffect(op uint32) atomicEffect {
	if op == 0 {
		op = 30
	}
	pos := (op - 30) % 7
	if pos == 0 {
		return atomicEffect{typ: effectI32, align: 2}
	}
	if pos == 1 {
		return atomicEffect{typ: effectI64, align: 3}
	}
	if pos == 2 || pos == 3 {
		return atomicEffect{typ: effectI32, align: uint8(pos - 2)}
	}
	return atomicEffect{typ: effectI64, align: uint8(pos - 4)}
}

func atomicCmpxchgEffect(op uint32) atomicEffect {
	if op == 0 {
		op = 72
	}
	switch op {
	case 72:
		return atomicEffect{typ: effectI32, align: 2}
	case 73:
		return atomicEffect{typ: effectI64, align: 3}
	case 74, 75:
		return atomicEffect{typ: effectI32, align: uint8(op - 74)}
	default:
		return atomicEffect{typ: effectI64, align: uint8(op - 76)}
	}
}
