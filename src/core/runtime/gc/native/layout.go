// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package gc

import (
	"errors"
	"fmt"
	"unsafe"
)

// ObjHeader is the stable guest-object header stored in the byte heap before
// every payload. It is represented here for layout documentation/tests; heap
// memory stores the fields little-endian, not as Go heap objects.
type ObjHeader struct {
	TypeID uint32 // exact canonical TypeID
	Size   uint32 // total object size including header
	Aux    uint32 // array length, or forwarding metadata during copying
	Flags  uint32 // generation/color/age/pointer-free/forwarding bits
}

const (
	FlagPointerFree uint32 = 1 << iota
	FlagForwarded
	FlagMarked
	FlagOld
	FlagLarge
)

const HeaderSize = uint32(unsafe.Sizeof(ObjHeader{}))
const PayloadOffset = HeaderSize

// The type projection uses spare bits in the existing object flags word. The
// TypeID word remains exact, so concrete equality needs one raw load and all
// uint32 canonical IDs retain their identity.
const (
	FlagTypeStruct    uint32 = 1 << 24
	FlagTypeArray     uint32 = 1 << 25
	FlagTypeFinal     uint32 = 1 << 26
	FlagTypeFastValid uint32 = 1 << 31
	FlagTypeAny              = FlagTypeStruct | FlagTypeArray
	FlagTypeEq               = FlagTypeAny
)

// ObjectTypeFlags derives the immutable header projection from one validated
// descriptor. It is the only production constructor for these flag bits.
func ObjectTypeFlags(d TypeDesc) uint32 {
	if d.Kind != KindStruct && d.Kind != KindArray {
		return 0
	}
	flags := uint32(FlagTypeFastValid)
	if d.Final {
		flags |= FlagTypeFinal
	}
	if d.Kind == KindStruct {
		return flags | FlagTypeStruct
	}
	return flags | FlagTypeArray
}

// ErrAllocationTooLarge reports an object whose physical aligned extent cannot
// be represented by the collector's uint32 heap ABI.
var ErrAllocationTooLarge = errors.New("gc: allocation size too large")

func Align8(v uint32) uint32  { return align(v, 8) }
func Align16(v uint32) uint32 { return align(v, 16) }

func makeAlignedBytes(size uint32, alignment uintptr) []byte {
	if size == 0 {
		return nil
	}
	raw := make([]byte, uint64(size)+uint64(alignment-1))
	base := uintptr(unsafe.Pointer(&raw[0]))
	delta := (alignment - base%alignment) % alignment
	start := int(delta)
	end := start + int(size)
	return raw[start:end:end]
}

func requiredObjectAlignment(types []TypeDesc) uint32 {
	alignment := uint32(8)
	for _, d := range types {
		if (d.Kind == KindStruct || d.Kind == KindArray) && d.Align > alignment {
			alignment = d.Align
		}
	}
	return alignment
}
func StructSize(d TypeDesc) (uint32, error) {
	if d.Kind != KindStruct {
		return 0, fmt.Errorf("gc: type %d is not a struct", d.ID)
	}
	total := uint64(HeaderSize) + uint64(d.Size)
	if total > uint64(^uint32(0)-7) {
		return 0, fmt.Errorf("gc: struct size overflow: %w", ErrAllocationTooLarge)
	}
	return Align8(uint32(total)), nil
}
func ArraySize(d TypeDesc, length uint32) (uint32, error) {
	if d.Kind != KindArray {
		return 0, fmt.Errorf("gc: type %d is not an array", d.ID)
	}
	payload := uint64(d.ElemSize) * uint64(length)
	total := uint64(HeaderSize) + payload
	if total > uint64(^uint32(0)-7) {
		return 0, fmt.Errorf("gc: array size overflow: %w", ErrAllocationTooLarge)
	}
	return Align8(uint32(total)), nil
}
