//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"math/bits"
	"unsafe"
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
	switch helper {
	case gcStructTableSet:
		return len(args) == 3 && in.compactExternTable(args[2])
	case gcTableFill, gcTableCopy, gcTableInit:
		return len(args) >= 4 && in.compactExternTable(args[3])
	case gcTableGrow:
		return len(args) == 3 && in.compactExternTable(args[2])
	}
	return false
}

func (in *Instance) dispatchCompactExternTableBulk(helper uint32, args, results []uint64, domain *gcStoreDomain, roots gc.RootSet) bool {
	var target uint64
	if helper == gcTableElemDrop {
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
	switch helper {
	case gcTableFill:
		if len(args) != 4 || len(results) != 0 {
			return false
		}
		target = args[3]
	case gcTableCopy, gcTableInit:
		if len(args) != 5 || len(results) != 0 {
			return false
		}
		target = args[3]
	case gcTableGrow:
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
	if helper == gcTableGrow {
		destination, count = uint64(length), args[1]
		if count > uint64(capacity)-uint64(length) {
			results[0] = uint64(^uint32(0))
			return true
		}
	} else if destination > uint64(length) || count > uint64(length)-destination {
		panic(gcHelperTrap(coreruntime.TrapTableOutOfBounds))
	}
	if count == 0 && (helper == gcTableFill || helper == gcTableGrow) {
		if helper == gcTableGrow {
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
	if helper == gcTableFill || helper == gcTableGrow {
		word := args[1]
		if helper == gcTableGrow {
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
	} else {
		var sourceBytes []byte
		source := args[1]
		if helper == gcTableCopy {
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
		} else {
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
	}
	if helper == gcTableGrow {
		binary.LittleEndian.PutUint32(desc, uint32(uint64(length)+count))
		results[0] = uint64(length)
	}
	return true
}

func gcRetainedTableDescriptor(in *Instance, index int) []byte {
	return rv64RetainedTableDescriptor(in, index)
}
