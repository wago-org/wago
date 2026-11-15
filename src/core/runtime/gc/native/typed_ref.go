// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package gc

import "errors"

// Type words are descriptor projections indexed by the actual dense TypeID.
// The low sixteen bits are meaningful only while FastValid is set. Nullability
// and exactness describe a use of a reference, not its heap descriptor.
const (
	NativeTypeWordIDMask    uint32 = 0xffff
	NativeTypeWordKindShift        = 16
	NativeTypeWordFinal     uint32 = 1 << 18
	NativeTypeWordLeaf      uint32 = 1 << 19
	NativeTypeWordAny       uint32 = 1 << 20
	NativeTypeWordEq        uint32 = 1 << 21
	NativeTypeWordStruct    uint32 = 1 << 22
	NativeTypeWordArray     uint32 = 1 << 23
	NativeTypeWordFunc      uint32 = 1 << 24
	NativeTypeWordFastValid uint32 = 1 << 25
	NativeTypedRefKnown     uint64 = 1 << 48
)

// TypedRef is an internal native value, not a public reference token. Its low
// word is the unchanged compact Ref, bits32..47 carry its actual dense TypeID,
// and bit48 proves that the ID is present. Null and i31 remain untagged.
//
// The owning adapter must keep the original reference live and in this
// collector domain. As with Ref, this word does not authenticate ownership or
// handle generations. It never grants permission for physical object access.
// Do not reconstruct it from public tokens, opaque function/extern values,
// globals, or heap words. Those boundaries must use UnknownTypedRef or perform
// the existing ownership conversion before TypedReference.
type TypedRef struct{ word uint64 }

// UnknownTypedRef preserves a compact reference without claiming a type ID.
func UnknownTypedRef(r Ref) TypedRef { return TypedRef{word: uint64(r)} }

func (r TypedRef) Ref() Ref { return Ref(uint32(r.word)) }

// NativeWord exposes the internal carrier only to native ABI adapters.
func (r TypedRef) NativeWord() uint64 { return r.word }

// TypeIndex returns the direct dense index. The validity result distinguishes
// unknown from both type0 and the valid type65535.
func (r TypedRef) TypeIndex() (uint16, bool) {
	if r.word&NativeTypedRefKnown == 0 {
		return 0xffff, false
	}
	return uint16(r.word >> 32), true
}

// Equal compares object/immediate identity independent of type knowledge.
// Both operands must belong to the same collector domain.
func (r TypedRef) Equal(other TypedRef) bool { return r.Ref() == other.Ref() }

func projectTypeWord(d TypeDesc) uint32 {
	word := uint32(d.ID)&NativeTypeWordIDMask | uint32(d.Kind)<<NativeTypeWordKindShift | NativeTypeWordLeaf
	if d.Final {
		word |= NativeTypeWordFinal
	}
	if !d.GuestType {
		return word
	}
	switch d.Kind {
	case KindStruct:
		word |= NativeTypeWordAny | NativeTypeWordEq | NativeTypeWordStruct
	case KindArray:
		word |= NativeTypeWordAny | NativeTypeWordEq | NativeTypeWordArray
	case KindFunc:
		return word | NativeTypeWordFunc
	default:
		return word
	}
	if d.ID <= TypeID(NativeTypeWordIDMask) {
		word |= NativeTypeWordFastValid
	}
	return word
}

// appendTypeWords runs only after registration has validated and committed the
// descriptor graph. Existing rows change only when a new child clears Leaf.
// There is one fixed-width row per type, with no table-size bit packing.
func (c *Collector) appendTypeWords(oldCount int) {
	c.typeWords = append(c.typeWords, make([]uint32, len(c.types)-oldCount)...)
	for i := oldCount; i < len(c.types); i++ {
		c.typeWords[i] = projectTypeWord(c.types[i])
	}
	for _, d := range c.types[oldCount:] {
		if d.HasSuper {
			c.typeWords[d.Super] &^= NativeTypeWordLeaf
		}
	}
}

var errTypedObject = errors.New("gc: invalid typed object ref")

// typedObjectLive retains the handle, space, size and extent checks without
// loading the heap object's type header. The type of a live admitted object is
// immutable; moving collection preserves its handle and actual TypeID.
func (c *Collector) typedObjectLive(r Ref) bool {
	if !c.validObjectRef(r) {
		return false
	}
	e := &c.handles[handleOf(r)]
	var bytes uint64
	switch e.space {
	case spaceNursery:
		bytes = uint64(len(c.nursery))
	case spaceOld, spaceLarge:
		bytes = uint64(len(c.throughput.mem))
	case spaceTiny:
		bytes = uint64(len(c.tiny.mem))
	default:
		return false
	}
	return e.size >= HeaderSize && uint64(e.off) <= bytes && uint64(e.size) <= bytes-uint64(e.off)
}

// TypedReference establishes a carrier from an owned compact reference. It
// performs the existing checked descriptor resolution once. Overflow and
// private types retain their full-ID checked fallback with no truncated ID.
func (c *Collector) TypedReference(r Ref) (TypedRef, error) {
	if err := c.errIfClosed(); err != nil {
		return TypedRef{}, err
	}
	result := UnknownTypedRef(r)
	if !r.IsObj() {
		return result, nil
	}
	if !c.typedObjectLive(r) {
		return TypedRef{}, errTypedObject
	}
	d, err := c.refDescPtr(r)
	if err != nil {
		return TypedRef{}, err
	}
	if c.typeWords[d.ID]&NativeTypeWordFastValid != 0 {
		result.word |= uint64(d.ID)<<32 | NativeTypedRefKnown
	}
	return result, nil
}

// RefTestTyped answers pure type questions using the carrier and current type
// metadata. It does not resolve the object address on a known fast path.
// Runtime-domain canonical representative maps must continue to use
// RefTestCanonical; this method must not replace their equality semantics.
func (c *Collector) RefTestTyped(r TypedRef, target RefTestTarget) (bool, error) {
	if err := c.errIfClosed(); err != nil {
		return false, err
	}
	defined, err := c.refTestTargetDesc(target)
	if err != nil {
		return false, err
	}
	index, known := r.TypeIndex()
	if !known {
		return c.refTest(r.Ref(), target, nil)
	}
	if !c.typedObjectLive(r.Ref()) || int(index) >= len(c.typeWords) {
		return false, errTypedObject
	}
	word := c.typeWords[index]
	if word&NativeTypeWordFastValid == 0 {
		return c.refTest(r.Ref(), target, nil)
	}
	switch target.Kind {
	case RefTestDefined:
		if TypeID(index) == target.Type {
			return true, nil
		}
		if target.Exact || defined.Final {
			return false, nil
		}
		actual, required := c.subtypeIntervals[index], c.subtypeIntervals[target.Type]
		return uint32(actual>>32) >= uint32(required>>32) && uint32(actual) <= uint32(required), nil
	case RefTestAny:
		return word&NativeTypeWordAny != 0, nil
	case RefTestEq:
		return word&NativeTypeWordEq != 0, nil
	case RefTestStruct:
		return word&NativeTypeWordStruct != 0, nil
	case RefTestArray:
		return word&NativeTypeWordArray != 0, nil
	default:
		return false, nil
	}
}

// RefCastTyped retains the original carrier when the checked target matches.
func (c *Collector) RefCastTyped(r TypedRef, target RefTestTarget) (TypedRef, error) {
	match, err := c.RefTestTyped(r, target)
	if err != nil {
		return TypedRef{}, err
	}
	if !match {
		return TypedRef{}, ErrCastFailure
	}
	return r, nil
}
