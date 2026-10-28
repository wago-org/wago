//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"math/bits"
	"unsafe"
)

// Persistent native words stay opaque. Qualified writes root the local shell
// or shared host anchor; legacy noncollecting construction can retain a direct pin.
// Cell indexes never move while Tiny can retain a root cursor.
type compactExternPersistentCell struct {
	key  uintptr
	word uint64
	root gc.Root
	pin  uint64
}
type compactExternPersistentOwner struct {
	cells   []compactExternPersistentCell
	indexes map[uintptr]uint32
}

// The caller holds store.mu; released generations cannot name a live value.
func (s *referenceStore) externrefValueLocked(token uint64) (any, bool) {
	if token == 0 || s.externKey == 0 {
		return nil, false
	}
	raw := bits.RotateLeft64(token, -17) ^ s.externKey
	index, generation := uint32(raw), uint32(raw>>32)
	if index == 0 || uint64(index) > uint64(len(s.externrefs)) {
		return nil, false
	}
	slot := &s.externrefs[index-1]
	return slot.value, slot.generation == generation && slot.value != releasedExternrefValue
}

func (s *compactExternCollector) persistentWord(word uint64) (uint64, gc.Ref, uint64, error) {
	if word == 0 {
		return 0, gc.Null(), 0, nil
	}
	collector, ref := compactExternWordRoot(s.store, word)
	if collector == nil {
		s.store.mu.Lock()
		value, ok := s.store.externrefValueLocked(word)
		converted, convertedOK := value.(interface{ nativeExternWord() uint64 })
		s.store.mu.Unlock()
		if !ok {
			return 0, gc.Null(), 0, fmt.Errorf("persistent extern has no live identity")
		}
		if convertedOK {
			word = converted.nativeExternWord()
			collector, ref = compactExternWordRoot(s.store, word)
			if collector == nil {
				return 0, gc.Null(), 0, fmt.Errorf("persistent converted extern lost its shell")
			}
		} else {
			if !s.store.pinExternref(word) {
				return 0, gc.Null(), 0, fmt.Errorf("persistent foreign extern was released")
			}
			return word, gc.Null(), word, nil
		}
	}
	if collector != s.collector {
		return 0, gc.Null(), 0, fmt.Errorf("persistent extern belongs to a different collector domain")
	}
	return word, ref, 0, nil
}

// The caller authenticates the exact native cell before this transaction and
// writes the returned word before it releases the collector-domain lock.
func (s *compactExternCollector) storePersistent(owner *Instance, key uintptr, word uint64, roots ...gc.RootSet) (uint64, error) {
	if owner == nil || owner.gc != s.collector || owner.refStore != s.store || key == 0 {
		return 0, fmt.Errorf("persistent extern has no exact native owner")
	}
	rows := s.owners[owner]
	if rows != nil {
		if index, ok := rows.indexes[key]; ok && rows.cells[index].word == word && (len(roots) == 0 || word == 0 || !rows.cells[index].root.GetRef().IsNull()) {
			return word, nil
		}
	} else if word == 0 {
		return 0, nil
	}
	var prepared compactExternPreparedWord
	var err error
	if len(roots) != 0 {
		prepared, err = s.preparePersistent(word, roots[0])
	} else {
		prepared.word, prepared.ref, prepared.pin, err = s.persistentWord(word)
	}
	if err != nil {
		return 0, err
	}
	normalized, pin := prepared.word, prepared.pin
	if !s.storePrepared(owner, key, &prepared) && pin != 0 {
		if !s.store.unpinExternref(pin) {
			panic("wago: persistent extern preparation lost its pin")
		}
	}
	return normalized, nil
}

type compactExternPreparedWord struct {
	word uint64
	ref  gc.Ref
	pin  uint64
}

// Validation pins protect the host slot while its shared anchor allocates.
// Once published, a persistent root or raw native extern root traces the anchor.
func (s *compactExternCollector) preparePersistent(word uint64, roots gc.RootSet) (compactExternPreparedWord, error) {
	normalized, ref, pin, err := s.persistentWord(word)
	if err != nil {
		return compactExternPreparedWord{}, err
	}
	if pin != 0 {
		defer func() {
			if !s.store.unpinExternref(pin) {
				panic("wago: extern preparation pin changed")
			}
		}()
		ref, err = s.anyFromForeign(normalized, roots)
		if err != nil {
			return compactExternPreparedWord{}, err
		}
	}
	return compactExternPreparedWord{word: normalized, ref: ref}, nil
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

// A false result leaves the prepared pin with its caller. Existing indexes
// remain stable while Tiny can hold an object/owner root cursor.
func (s *compactExternCollector) storePrepared(owner *Instance, key uintptr, value *compactExternPreparedWord) bool {
	rows := s.owners[owner]
	if rows == nil {
		if value.word == 0 {
			return false
		}
		if s.owners == nil {
			s.owners = make(map[*Instance]*compactExternPersistentOwner)
		}
		rows = &compactExternPersistentOwner{indexes: make(map[uintptr]uint32)}
		s.owners[owner] = rows
	}
	index, exists := rows.indexes[key]
	if exists && rows.cells[index].word == value.word {
		return false
	}
	if !exists {
		index = uint32(len(rows.cells))
		rows.indexes[key] = index
		rows.cells = append(rows.cells, compactExternPersistentCell{key: key})
	}
	old := rows.cells[index].pin
	rows.cells[index] = compactExternPersistentCell{key: key, word: value.word, root: gc.Root(value.ref), pin: value.pin}
	value.pin = 0
	s.collector.WriteBarrierRoot(value.ref)
	if old != 0 {
		s.retirePersistentPin(old)
	}
	return true
}

func rangeLocalCompactExternRoots(in *Instance, fn func(gc.RootSlot) bool, sink gc.RootRefSink) bool {
	if in == nil || compactExternCollectorCount.Load() == 0 {
		return true
	}
	state, ok := compactExternCollectors.Load(in.gc)
	if !ok {
		return true
	}
	rows := state.owners[in]
	if rows == nil {
		return true
	}
	for i := range rows.cells {
		slot := &rows.cells[i].root
		if sink != nil {
			if !sink.VisitRootRef(slot.GetRef()) {
				return false
			}
		} else if !fn(slot) {
			return false
		}
	}
	return true
}

func releaseInstanceCompactExtern(in *Instance) {
	if in == nil || compactExternCollectorCount.Load() == 0 {
		return
	}
	value, ok := compactExternCollectors.Load(in.gc)
	if !ok {
		return
	}
	s := value
	unlockNative := lockNativeExecutionForHostAccess()
	domain := in.lockGCCollector()
	if rows := s.owners[in]; rows != nil {
		for _, cell := range rows.cells {
			if cell.pin != 0 {
				s.retirePersistentPin(cell.pin)
			}
		}
		delete(s.owners, in)
	}
	unlockGCCollector(domain)
	unlockNative()
	s.drain()
}

const gcExternGlobalSet uint32 = 47

func (in *Instance) dispatchCompactExternStore(ctrl uintptr, helper, safepoint uint32, args, results []uint64, stack *coreruntime.Engine) bool {
	if helper != gcExternGlobalSet {
		return false
	}
	if in == nil || in.c == nil || in.gc == nil || !in.c.compactExternFrameRoots() || len(args) != 2 || len(results) != 0 || args[1] >= uint64(len(in.c.Globals)) || args[1] >= uint64(len(in.globalCells)) {
		panic(gcHelperFailuref("extern global store has no exact collector metadata"))
	}
	index := int(args[1])
	global := in.c.Globals[index]
	exact, err := in.c.globalExactType(index)
	if err != nil || !global.Mutable || global.Type != ValExternRef || exact.Kind != ValueTypeReference || exact.Ref.Exact || exact.Ref.Heap.Defined || exact.Ref.Heap.Abstract != AbstractHeapExtern || !exact.Ref.Nullable && args[0] == 0 || in.globalCells[index] == nil || len(in.globalCells[index].cell) != 8 {
		panic(gcHelperFailuref("extern global store type or cell mismatch"))
	}
	defer drainCompactExternReclaims(in.gc)
	domain := in.lockGCCollector()
	defer unlockGCCollector(domain)
	state, err := in.compactExternCollector(domain)
	if err != nil {
		panic(gcHelperFailure(err))
	}
	in.gc.CancelNativeAllocationBatch()
	public := in.publicGCState()
	public.mu.Lock()
	defer public.mu.Unlock()
	defer func() { public.frameRoots.physicalStack = nil }()
	roots := in.gcHelperRoots(ctrl, public, safepoint, stack)
	storageOwner := in
	if owner := in.globalCells[index].owner; owner != nil && owner.instance != nil {
		storageOwner = owner.instance
	}
	cell := in.globalCells[index].cell
	word := uint64(0)
	if host := compactExternHostGlobalRow(in.globalCells[index].owner); host != nil {
		value, prepareErr := state.preparePersistent(args[0], roots)
		err = prepareErr
		if err == nil {
			state.storeHostPrepared(&host.compactExternHostRootRow, uintptr(unsafe.Pointer(&cell[0])), &value, 0)
			word = value.word
		}
	} else {
		word, err = state.storePersistent(storageOwner, uintptr(unsafe.Pointer(&cell[0])), args[0], roots)
	}
	if err != nil {
		panic(gcHelperFailure(err))
	}
	binary.LittleEndian.PutUint64(cell, word)
	return true
}

func (s *compactExternCollector) retirePersistentPin(token uint64) {
	s.mu.Lock()
	s.reclaimed = append(s.reclaimed, token)
	s.mu.Unlock()
}

func (in *Instance) storeCompactExternTable(table, index, word uint64, domain *gcStoreDomain, roots gc.RootSet) bool {
	if in == nil || in.c == nil || table >= uint64(in.c.tableCount()) || in.c.tableElementType(int(table)) != ValExternRef || !in.c.compactExternFrameRoots() {
		return false
	}
	desc, length, _, exact, owner, host := in.gcRootedTablePhysicalStorage(table)
	if index >= uint64(length) {
		panic(gcHelperTrap(coreruntime.TrapTableOutOfBounds))
	}
	if exact.Ref.Heap.Abstract == AbstractHeapNoExtern && word != 0 {
		panic(gcHelperFailuref("non-null extern in bottom table"))
	}
	state, err := in.compactExternCollector(domain)
	if err != nil {
		panic(gcHelperFailure(err))
	}
	cell := desc[8+index*8 : 8+(index+1)*8]
	value, err := state.preparePersistent(word, roots)
	if err != nil {
		panic(gcHelperFailure(err))
	}
	state.storePhysicalPrepared(owner, host, uintptr(unsafe.Pointer(&cell[0])), &value)
	binary.LittleEndian.PutUint64(cell, value.word)
	return true
}
