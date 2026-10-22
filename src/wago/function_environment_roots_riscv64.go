//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func managedFunctionOwnerRootsStrong(owner *Instance, collector *gc.Collector) bool {
	if managedFunctionMemberCount.Load() == 0 {
		return true
	}
	value, ok := managedFunctionMembers.Load(owner)
	if !ok {
		return true
	}
	member := value.(*managedFunctionMember)
	if member.collecting.Load() {
		return false
	}
	state, ok := managedFunctionCollectors.Load(collector)
	if !ok {
		return true
	}
	s := state.(*managedFunctionCollector)
	return !s.authorized || s.liveOwners[member]
}

// Only private instance-root mappings are conditional. Public-token roots,
// argument/result roots, host slots and clone temporaries are deliberately absent.
func (s *managedFunctionCollector) bindOwnerSlots(owner *Instance) error {
	if public := owner.existingPublicGCState(); public != nil {
		for _, mapping := range public.globalRoots {
			if err := s.bindPersistentSlot(owner, gc.SlotGlobal, mapping.SlotIndex); err != nil {
				return err
			}
		}
	}
	if plugin := owner.pluginState.Load(); plugin != nil {
		for i := uint32(0); i < plugin.gcGlobalRootCount; i++ {
			if err := s.bindPersistentSlot(owner, gc.SlotGlobal, plugin.gcGlobalRoots[i].SlotIndex); err != nil {
				return err
			}
		}
		if elements := plugin.gcArrayElements.Load(); elements != nil {
			for _, slot := range elements.Slots {
				if err := s.bindPersistentSlot(owner, gc.SlotTable, slot); err != nil {
					return err
				}
			}
		}
		if tables := plugin.gcRefTestTable.Load(); tables != nil {
			for _, slots := range tables.slots {
				for _, slot := range slots {
					if err := s.bindPersistentSlot(owner, gc.SlotTable, slot); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

type managedFunctionNativeRootCursor struct {
	phase   uint8
	index   uint32
	element uint64
}

// traceNativeOwnerRoot reads at most one native global, table header/entry, or
// passive-element header/entry. Refs are resolved only in this owner's collector.
func (m *managedFunctionMark) traceNativeOwnerRoot(owner *Instance, cursor *managedFunctionNativeRootCursor) (bool, error) {
	if owner == nil || owner.gc == nil || owner.c == nil {
		return true, nil
	}
	d := m.byCollector[owner.gc]
	if d == nil {
		return false, fmt.Errorf("native owner root has no participating collector")
	}
	visit := func(bits uint64) error {
		ref := gc.Ref(uint32(bits))
		if bits != uint64(ref) {
			return fmt.Errorf("native owner root is not compact")
		}
		d.VisitObjectRef(ref)
		if m.err == nil && ref.IsObj() {
			// The independent ownership mark does not set collector mark bits.
			// Retain each live native root for the subsequent collector pass.
			d.nativeRoots = append(d.nativeRoots, gc.Root(ref))
		}
		return m.err
	}
	switch cursor.phase {
	case 0:
		if int(cursor.index) < len(owner.globalCells) {
			index := cursor.index
			cursor.index++
			cell := owner.globalCells[index]
			if cell != nil && int(index) < len(owner.c.Globals) && isGCRefValType(owner.c.Globals[index].Type) {
				if len(cell.cell) < 8 {
					return false, fmt.Errorf("native owner global is truncated")
				}
				return false, visit(binary.LittleEndian.Uint64(cell.cell))
			}
			if cell != nil && int(index) < len(owner.c.GlobalImports) && int(index) < len(owner.c.Globals) && owner.c.Globals[index].Type == ValExternRef && owner.c.compactExternFrameRoots() {
				if len(cell.cell) != 8 {
					return false, fmt.Errorf("native owner extern global is truncated")
				}
				_, ref := compactExternFrameRoot(owner, owner.gc, binary.LittleEndian.Uint64(cell.cell))
				return false, visit(uint64(ref))
			}
			return false, nil
		}
		cursor.phase = 1
		cursor.index = 0
		return false, nil
	case 1:
		if int(cursor.index) >= owner.c.tableCount() {
			cursor.phase = 2
			cursor.index = 0
			cursor.element = 0
			return false, nil
		}
		if !isGCRefValType(owner.c.tableElementType(int(cursor.index))) {
			cursor.index++
			cursor.element = 0
			return false, nil
		}
		// Exact abstract products use their checked collector slots above. Their
		// descriptor can also contain foreign conversion identities, not compact refs.
		if rv64GCRefTestAbstract(owner.c) || rv64GCExtern(owner.c) || rv64GCRefCastAbstract(owner.c) || rv64GCBrOnCastAbstract(owner.c) || rv64GCBrOnCastFailAbstract(owner.c) {
			cursor.index++
			cursor.element = 0
			return false, nil
		}
		desc := owner.tableDescriptor(int(cursor.index))
		if len(desc) < 8 {
			return false, fmt.Errorf("native owner table descriptor is unavailable")
		}
		length := uint64(binary.LittleEndian.Uint32(desc))
		if length > uint64((len(desc)-8)/8) {
			return false, fmt.Errorf("native owner table exceeds descriptor capacity")
		}
		if cursor.element >= length {
			cursor.index++
			cursor.element = 0
			return false, nil
		}
		bits := binary.LittleEndian.Uint64(desc[8+cursor.element*8:])
		cursor.element++
		return false, visit(bits)
	case 2:
		if int(cursor.index) >= len(owner.c.passiveElems) {
			cursor.phase, cursor.index, cursor.element = 3, 0, 0
			return false, nil
		}
		elem := &owner.c.passiveElems[cursor.index]
		if elem.Mode != ElemModePassive || !isGCRefValType(normalizedElemRefType(elem.RefType)) {
			cursor.index++
			cursor.element = 0
			return false, nil
		}
		if owner.nativeContext == 0 {
			return false, fmt.Errorf("native owner passive descriptor is unavailable")
		}
		// Read the retained owner context. Its JobMemory can currently be bound
		// to a different instance that shares the same memory.
		context := unsafe.Slice((*byte)(offHeapPtr(owner.nativeContext)), coreruntime.InstanceContextBytes)
		passive := uintptr(binary.LittleEndian.Uint64(context[24:]))
		if passive == 0 {
			return false, fmt.Errorf("native owner passive descriptor is unavailable")
		}
		addr := passive + uintptr(cursor.index)*coreruntime.PassiveElemDescBytes
		desc := unsafe.Slice((*byte)(offHeapPtr(addr)), coreruntime.PassiveElemDescBytes)
		entries, length := uintptr(binary.LittleEndian.Uint64(desc)), uint64(binary.LittleEndian.Uint32(desc[8:]))
		if length > uint64(len(elem.Values)) || length != 0 && entries == 0 {
			return false, fmt.Errorf("native owner passive descriptor is malformed")
		}
		if cursor.element >= length {
			cursor.index++
			cursor.element = 0
			return false, nil
		}
		bits := binary.LittleEndian.Uint64(unsafe.Slice((*byte)(offHeapPtr(entries+uintptr(cursor.element*8))), 8))
		cursor.element++
		return false, visit(bits)
	case 3:
		if compactExternCollectorCount.Load() == 0 {
			cursor.phase, cursor.index, cursor.element = 4, 0, 0
			return false, nil
		}
		value, ok := compactExternCollectors.Load(owner.gc)
		if !ok {
			cursor.phase, cursor.index, cursor.element = 4, 0, 0
			return false, nil
		}
		rows := value.owners[owner]
		if rows == nil || uint64(cursor.index) >= uint64(len(rows.cells)) {
			cursor.phase, cursor.index, cursor.element = 4, 0, 0
			return false, nil
		}
		ref := rows.cells[cursor.index].root.GetRef()
		cursor.index++
		return false, visit(uint64(ref))
	case 4:
		if int(cursor.index) >= owner.c.tableCount() {
			return true, nil
		}
		if owner.c.tableElementType(int(cursor.index)) != ValExternRef {
			cursor.index++
			cursor.element = 0
			return false, nil
		}
		if _, imported := owner.c.tableImportAt(int(cursor.index)); !imported {
			cursor.index++
			cursor.element = 0
			return false, nil
		}
		desc := gcRetainedTableDescriptor(owner, int(cursor.index))
		if len(desc) < 8 {
			return false, fmt.Errorf("native extern table descriptor is unavailable")
		}
		length := uint64(binary.LittleEndian.Uint32(desc))
		if length > uint64((len(desc)-8)/8) {
			return false, fmt.Errorf("native extern table exceeds storage")
		}
		if cursor.element >= length {
			cursor.index++
			cursor.element = 0
			return false, nil
		}
		word := binary.LittleEndian.Uint64(desc[8+cursor.element*8:])
		cursor.element++
		_, ref := compactExternFrameRoot(owner, owner.gc, word)
		return false, visit(uint64(ref))

	default:
		return false, fmt.Errorf("invalid native owner root cursor")
	}
}

type managedFunctionProducerRootSink interface{ VisitFunctionProducer(*Instance) bool }

func (d *managedFunctionMarkDomain) VisitFunctionProducer(owner *Instance) bool {
	d.mark.graph.mu.Lock()
	member := d.mark.graph.members[owner]
	d.mark.graph.mu.Unlock()
	if member == nil {
		d.mark.err = fmt.Errorf("function frame producer has no joint owner member")
		return false
	}
	d.mark.visitOwner(member)
	return true
}

// A joint mark owns persistent roots through its owner queue. This view keeps
// the existing exact native chains and parked host activations without also
// importing every physically retained owner's persistent globals and tables.
type managedFunctionTransientRoots struct{ roots *gcNativeFrameRoots }

func (r managedFunctionTransientRoots) RangeRoots(fn func(gc.RootSlot) bool) {
	r.roots.rangeTransientRoots(fn, nil)
}
func (r managedFunctionTransientRoots) RangeRootRefs(sink gc.RootRefSink) bool {
	return r.roots.rangeTransientRoots(nil, sink)
}
