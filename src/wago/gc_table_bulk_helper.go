// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import (
	"encoding/binary"
	"unsafe"

	"github.com/wago-org/wago/internal/buildfeatures"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

const collectorTableBulkBuilt = buildfeatures.BulkMemoryOperations != 0 && buildfeatures.ReferenceTypes != 0

func (in *Instance) dispatchCollectorTableBulkHelperLocked(helper uint32, args []uint64) {
	switch {
	case gcTableInitForImage && helper == gcTableInit:
		if len(args) != 5 {
			panic(gcHelperFailuref("gc table init helper arity = %d, want 5", len(args)))
		}
		descriptor, tableLength, _, exact := in.gcRootedTableDescriptor(args[3])
		destination, source, count := args[0], args[1], args[2]
		if destination+count < destination || destination+count > uint64(tableLength) {
			panic(gcHelperTrap(coreruntime.TrapTableOutOfBounds))
		}
		entries, segmentLength := in.gcRootedTableElementSegment(uint32(args[4]), exact)
		if source+count < source || source+count > uint64(segmentLength) {
			panic(gcHelperTrap(coreruntime.TrapTableOutOfBounds))
		}
		// Authenticate every retained source root before publishing the first
		// destination write. This keeps trapping/corrupt metadata atomic.
		for i := uint64(0); i < count; i++ {
			word := in.gcRootedTableElementBits(entries, source+i)
			_ = in.gcRootedTableReference(word, exact)
		}
		for i := uint64(0); i < count; i++ {
			ref := in.gcRootedTableReference(in.gcRootedTableElementBits(entries, source+i), exact)
			binary.LittleEndian.PutUint64(descriptor[8+(destination+i)*8:], uint64(ref))
			in.gc.WriteBarrierRoot(ref)
		}
	case gcTableElemDropForImage && helper == gcTableElemDrop:
		if len(args) != 1 {
			panic(gcHelperFailuref("gc table elem-drop helper arity = %d, want 1", len(args)))
		}
		desc, _ := in.gcRootedTableElementDescriptor(uint32(args[0]))
		// Removing the descriptor length under the collector-domain lock makes
		// the passive roots unreachable atomically from the scanner's view.
		binary.LittleEndian.PutUint32(desc[8:], 0)
	case gcTableCopyForImage && helper == gcTableCopy:
		if len(args) != 5 {
			panic(gcHelperFailuref("gc table copy helper arity = %d, want 5", len(args)))
		}
		destination, destinationLength, _, destinationType := in.gcRootedTableDescriptor(args[3])
		source, sourceLength, _, _ := in.gcRootedTableDescriptor(args[4])
		destinationStart, sourceStart, count := args[0], args[1], args[2]
		if destinationStart+count < destinationStart || destinationStart+count > uint64(destinationLength) ||
			sourceStart+count < sourceStart || sourceStart+count > uint64(sourceLength) {
			panic(gcHelperTrap(coreruntime.TrapTableOutOfBounds))
		}
		// Validate the full source against the destination's exact table type
		// before the first write. copy then provides memmove semantics for an
		// overlapping same-table range.
		for i := uint64(0); i < count; i++ {
			word := binary.LittleEndian.Uint64(source[8+(sourceStart+i)*8:])
			_ = in.gcRootedTableReference(word, destinationType)
		}
		destinationBytes := destination[8+destinationStart*8 : 8+(destinationStart+count)*8]
		sourceBytes := source[8+sourceStart*8 : 8+(sourceStart+count)*8]
		copy(destinationBytes, sourceBytes)
		for i := uint64(0); i < count; i++ {
			in.gc.WriteBarrierRoot(gc.Ref(uint32(binary.LittleEndian.Uint64(destinationBytes[i*8:]))))
		}
	}
}

func (in *Instance) gcRootedTableElementDescriptor(elemIndex uint32) ([]byte, ElemInit) {
	if int(elemIndex) >= len(in.c.passiveElems) || in.jm.PassiveElemPtr() == 0 {
		panic(gcHelperFailuref("generic GC table element segment %d is unavailable", elemIndex))
	}
	elem := in.c.passiveElems[elemIndex]
	if elem.Mode != ElemModePassive && (elem.Mode != ElemModeActive && elem.Mode != ElemModeDeclarative || len(elem.Values) != 0) || elemEntryBytes(elem.RefType) != 8 || !isGCRefValType(normalizedElemRefType(elem.RefType)) && (!in.c.compactExternFrameRoots() || normalizedElemRefType(elem.RefType) != ValExternRef) {
		panic(gcHelperFailuref("generic GC table element segment %d is not passive aggregate reference data", elemIndex))
	}
	descOff := int(elemIndex) * coreruntime.PassiveElemDescBytes
	desc := unsafe.Slice((*byte)(offHeapPtr(in.jm.PassiveElemPtr()+uintptr(descOff))), coreruntime.PassiveElemDescBytes)
	return desc, elem
}

func (in *Instance) gcRootedTableElementSegment(elemIndex uint32, tableType ValueTypeDescriptor) (uintptr, uint32) {
	desc, elem := in.gcRootedTableElementDescriptor(elemIndex)
	exact, err := in.c.elemExactType(elem)
	if err != nil || !valueTypeSubtype(exact, in.c.Types, tableType, in.c.Types) {
		panic(gcHelperFailuref("generic GC table element segment %d type does not match destination", elemIndex))
	}
	length := binary.LittleEndian.Uint32(desc[8:])
	if uint64(length) > uint64(len(elem.Values)) {
		panic(gcHelperFailuref("generic GC table element segment %d length %d exceeds retained entries %d", elemIndex, length, len(elem.Values)))
	}
	entries := uintptr(binary.LittleEndian.Uint64(desc))
	if length != 0 && entries == 0 {
		panic(gcHelperFailuref("generic GC table element segment %d has no entries", elemIndex))
	}
	return entries, length
}

func (in *Instance) gcRootedTableElementBits(entries uintptr, index uint64) uint64 {
	if index > uint64(^uintptr(0))/8 || uint64(entries) > uint64(^uintptr(0))-index*8 {
		panic(gcHelperFailuref("generic GC table element entry %d address overflows", index))
	}
	entry := unsafe.Slice((*byte)(offHeapPtr(entries+uintptr(index*8))), 8)
	return binary.LittleEndian.Uint64(entry)
}
