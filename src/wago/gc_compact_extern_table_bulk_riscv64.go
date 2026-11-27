// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && !wago_no_bulk_memory_operations && !wago_no_reference_types

package wago

import (
	"encoding/binary"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

type compactExternTableBulkState struct {
	bulk      []compactExternPreparedWord
	bulkRoots *compactExternPreparedRoots
}

func (in *Instance) dispatchCompactExternTableBulk(helper uint32, args, results []uint64, domain *gcStoreDomain, roots gc.RootSet) bool {
	if compactExternElementsForImage && gcTableElemDropForImage && helper == gcTableElemDrop {
		if in == nil || in.c == nil || !in.c.compactExternFrameRoots() || len(args) != 1 || len(results) != 0 || args[0] >= uint64(len(in.c.passiveElems)) || normalizedElemRefType(in.c.passiveElems[args[0]].RefType) != ValExternRef {
			return false
		}
		desc, _ := in.gcRootedTableElementDescriptor(uint32(args[0]))
		length := binary.LittleEndian.Uint32(desc[8:])
		if length > uint32(len(in.c.passiveElems[args[0]].Values)) {
			panic(gcHelperFailuref("extern passive length exceeds retained metadata"))
		}
		if length != 0 {
			entries := uintptr(binary.LittleEndian.Uint64(desc))
			if entries == 0 {
				panic(gcHelperFailuref("extern passive entries unavailable"))
			}
			state, err := in.compactExternCollector(domain)
			if err != nil {
				panic(gcHelperFailure(err))
			}
			for i := uint32(0); i < length; i++ {
				address := entries + uintptr(i)*8
				if _, err := state.storePersistent(in, address, 0); err != nil {
					panic(gcHelperFailure(err))
				}
				binary.LittleEndian.PutUint64(unsafe.Slice((*byte)(offHeapPtr(address)), 8), 0)
			}
		}
		binary.LittleEndian.PutUint32(desc[8:], 0)
		return true
	}
	copyTable := gcTableCopyForImage && helper == gcTableCopy
	initTable := gcTableInitForImage && helper == gcTableInit
	if !compactExternTablesForImage || (!copyTable && !initTable) || len(args) != 5 || len(results) != 0 || !in.compactExternTable(args[3]) {
		return false
	}
	desc, length, _, exact, owner, host := in.gcRootedTablePhysicalStorage(args[3])
	destination, count := args[0], args[2]
	if destination > uint64(length) || count > uint64(length)-destination {
		panic(gcHelperTrap(coreruntime.TrapTableOutOfBounds))
	}
	state, err := in.compactExternCollector(domain)
	if err != nil {
		panic(gcHelperFailure(err))
	}
	if (owner == nil || owner.gc != in.gc || owner.refStore != in.refStore) && host == nil {
		panic(gcHelperFailuref("extern bulk table has no physical collector owner"))
	}
	var sourceBytes []byte
	source := args[1]
	if copyTable {
		if !in.compactExternTable(args[4]) {
			panic(gcHelperFailuref("extern table.copy has an incompatible source"))
		}
		from, sourceLength, _, sourceType, _ := in.gcRootedTableStorage(args[4])
		if !valueTypeSubtype(sourceType, in.c.Types, exact, in.c.Types) {
			panic(gcHelperFailuref("extern table.copy reference type"))
		}
		if source > uint64(sourceLength) || count > uint64(sourceLength)-source {
			panic(gcHelperTrap(coreruntime.TrapTableOutOfBounds))
		}
		sourceBytes = from[8+source*8 : 8+(source+count)*8]
	} else if gcTableInitForImage {
		if args[4] > uint64(^uint32(0)) {
			panic(gcHelperFailuref("extern passive index overflows"))
		}
		entries, sourceLength := in.gcRootedTableElementSegment(uint32(args[4]), exact)
		if source > uint64(sourceLength) || count > uint64(sourceLength)-source {
			panic(gcHelperTrap(coreruntime.TrapTableOutOfBounds))
		}
		if count != 0 {
			sourceBytes = unsafe.Slice((*byte)(offHeapPtr(entries+uintptr(source*8))), int(count*8))
		}
	}
	if count == 0 {
		return true
	}
	if uint64(cap(state.bulk)) < count {
		state.bulk = make([]compactExternPreparedWord, int(count))
	}
	prepared := state.bulk[:int(count)]
	if state.bulkRoots == nil {
		state.bulkRoots = &compactExternPreparedRoots{}
	}
	preparedRoots := state.bulkRoots
	if preparedRoots.parent != nil || preparedRoots.values != nil {
		panic("wago: reentrant extern bulk preparation")
	}
	preparedRoots.parent, preparedRoots.values = roots, prepared

	defer func() {
		preparedRoots.parent = nil
		preparedRoots.values = nil
		for i := range prepared {
			if prepared[i].pin != 0 && !state.store.unpinExternref(prepared[i].pin) {
				panic("wago: extern bulk prepared pin changed")
			}
			prepared[i] = compactExternPreparedWord{}
		}
	}()
	// No destination changes until all words and foreign pins are authenticated.
	for i := range prepared {
		word := binary.LittleEndian.Uint64(sourceBytes[i*8:])
		if exact.Ref.Heap.Abstract == AbstractHeapNoExtern && word != 0 {
			panic(gcHelperFailuref("non-null extern in bottom table"))
		}
		value, err := state.preparePersistent(word, preparedRoots)
		if err != nil {
			panic(gcHelperFailure(err))
		}
		prepared[i] = value
	}
	for i := range prepared {
		cell := desc[8+(destination+uint64(i))*8 : 8+(destination+uint64(i)+1)*8]
		state.storePhysicalPrepared(owner, host, uintptr(unsafe.Pointer(&cell[0])), &prepared[i])
		binary.LittleEndian.PutUint64(cell, prepared[i].word)
	}
	return true
}

type compactExternPreparedRoots struct {
	parent gc.RootSet
	values []compactExternPreparedWord
}

func (r *compactExternPreparedRoots) RangeRoots(fn func(gc.RootSlot) bool) {
	if r.parent != nil {
		more := true
		r.parent.RangeRoots(func(slot gc.RootSlot) bool { more = fn(slot); return more })
		if !more {
			return
		}
	}
	for i := range r.values {
		if !fn((*gc.Root)(&r.values[i].ref)) {
			return
		}
	}
}
func (r *compactExternPreparedRoots) RangeRootRefs(sink gc.RootRefSink) bool {
	if r.parent != nil {
		if direct, ok := r.parent.(gc.DirectRootRefSet); ok {
			if !direct.RangeRootRefs(sink) {
				return false
			}
		} else {
			more := true
			r.parent.RangeRoots(func(slot gc.RootSlot) bool { more = sink.VisitRootRef(slot.GetRef()); return more })
			if !more {
				return false
			}
		}
	}
	for _, value := range r.values {
		if !sink.VisitRootRef(value.ref) {
			return false
		}
	}
	return true
}
