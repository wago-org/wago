// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build linux && riscv64 && !tinygo && !wago_target_tinygo && (!wago_no_reference_types || !wago_no_gc || !wago_no_exception_handling)

package wago

import (
	"encoding/binary"
	"math/bits"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

// Bulk foreign pins are reserved while store.mu protects the slot generation.
// The reservation is either assigned to destination cells or returned in full.
func (s *referenceStore) compactExternPins(token uint64, count uint32, acquire bool) bool {
	if token == 0 || count == 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.externKey == 0 {
		return false
	}
	raw := bits.RotateLeft64(token, -17) ^ s.externKey
	index, generation := uint32(raw), uint32(raw>>32)
	if index == 0 || uint64(index) > uint64(len(s.externrefs)) {
		return false
	}
	slot := &s.externrefs[index-1]
	if slot.generation != generation || slot.value == releasedExternrefValue || slot.nextFree == externrefInternalSlot {
		return false
	}
	if acquire {
		if count >= externrefInternalSlot-slot.nextFree {
			return false
		}
		slot.nextFree += count
	} else {
		if slot.nextFree < count {
			return false
		}
		slot.nextFree -= count
	}
	return true
}

func (in *Instance) compactExternTable(table uint64) bool {
	return in != nil && in.c != nil && in.c.compactExternFrameRoots() && table < uint64(in.c.tableCount()) && in.c.tableElementType(int(table)) == ValExternRef
}

func (in *Instance) compactExternPersistentMayAllocate(helper uint32, args []uint64) bool {
	switch {
	case gcTableSetForImage && helper == gcStructTableSet, gcTableGrowForImage && helper == gcTableGrow:
		return len(args) == 3 && in.compactExternTable(args[2])
	case gcTableFillForImage && helper == gcTableFill,
		gcTableCopyForImage && helper == gcTableCopy, gcTableInitForImage && helper == gcTableInit:
		return len(args) >= 4 && in.compactExternTable(args[3])
	}
	return false
}

func (in *Instance) dispatchCompactExternTableWrite(helper uint32, args, results []uint64, domain *gcStoreDomain, roots gc.RootSet) bool {
	var target uint64
	switch {
	case gcTableFillForImage && helper == gcTableFill:
		if len(args) != 4 || len(results) != 0 {
			return false
		}
		target = args[3]
	case gcTableGrowForImage && helper == gcTableGrow:
		if len(args) != 3 || len(results) != 1 {
			return false
		}
		target = args[2]
	default:
		return false
	}
	if !in.compactExternTable(target) {
		return false
	}
	desc, length, capacity, exact, owner, host := in.gcRootedTablePhysicalStorage(target)
	destination, count := args[0], args[2]
	grow := gcTableGrowForImage && helper == gcTableGrow
	if grow {
		destination, count = uint64(length), args[1]
		if count > uint64(capacity)-uint64(length) {
			results[0] = uint64(^uint32(0))
			return true
		}
	} else if destination > uint64(length) || count > uint64(length)-destination {
		panic(gcHelperTrap(coreruntime.TrapTableOutOfBounds))
	}
	if count == 0 {
		if grow {
			results[0] = uint64(length)
		}
		return true
	}
	state, err := in.compactExternCollector(domain)
	if err != nil {
		panic(gcHelperFailure(err))
	}
	if (owner == nil || owner.gc != in.gc || owner.refStore != in.refStore) && host == nil {
		panic(gcHelperFailuref("extern bulk table has no physical collector owner"))
	}
	word := args[1]
	if grow {
		word = args[0]
	}
	if exact.Ref.Heap.Abstract == AbstractHeapNoExtern && word != 0 {
		panic(gcHelperFailuref("non-null extern in bottom table"))
	}
	value, err := state.preparePersistent(word, roots)
	normalized, ref, pin := value.word, value.ref, value.pin
	if err != nil {
		panic(gcHelperFailure(err))
	}
	remaining := uint32(0)
	if pin != 0 {
		remaining = 1
		defer func() {
			if remaining != 0 && !state.store.compactExternPins(pin, remaining, false) {
				panic("wago: extern bulk pin reservation changed")
			}
		}()
		if !state.store.compactExternPins(pin, uint32(count)-1, true) {
			panic(gcHelperFailuref("extern bulk pin capacity"))
		}
		remaining = uint32(count)
	}
	for i := uint64(0); i < count; i++ {
		cell := desc[8+(destination+i)*8 : 8+(destination+i+1)*8]
		value := compactExternPreparedWord{word: normalized, ref: ref, pin: pin}
		if state.storePhysicalPrepared(owner, host, uintptr(unsafe.Pointer(&cell[0])), &value) && pin != 0 {
			remaining--
		}
		binary.LittleEndian.PutUint64(cell, normalized)
	}
	if grow {
		binary.LittleEndian.PutUint32(desc, uint32(uint64(length)+count))
		results[0] = uint64(length)
	}
	return true
}

func gcRetainedTableDescriptor(in *Instance, index int) []byte {
	return rv64RetainedTableDescriptor(in, index)
}
