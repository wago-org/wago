// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || (darwin && arm64)) && !riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"fmt"
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

type managedFunctionNativeRootCursor struct{ done bool }

func (m *managedFunctionMark) traceNativeOwnerRoot(owner *Instance, cursor *managedFunctionNativeRootCursor) (bool, error) {
	if cursor.done || owner == nil || owner.gc == nil || owner.c == nil {
		return true, nil
	}
	cursor.done = true
	d := m.byCollector[owner.gc]
	if d == nil {
		return false, fmt.Errorf("native owner root has no participating collector")
	}
	visit := func(ref gc.Ref) bool {
		d.VisitObjectRef(ref)
		if m.err == nil && ref.IsObj() {
			d.nativeRoots = append(d.nativeRoots, gc.Root(ref))
		}
		return m.err == nil
	}
	for i, cell := range owner.globalCells {
		if cell != nil && i < len(owner.c.Globals) && isGCRefValType(owner.c.Globals[i].Type) {
			if len(cell.cell) < 8 {
				return false, fmt.Errorf("native owner global is truncated")
			}
			bits := binary.LittleEndian.Uint64(cell.cell)
			if bits != uint64(uint32(bits)) {
				return false, fmt.Errorf("native owner global is not compact")
			}
			if !visit(gc.Ref(bits)) {
				return false, m.err
			}
		}
	}
	owner.rangeLocalGCTableRoots(func(slot gc.RootSlot) bool { return visit(slot.GetRef()) }, nil)
	return true, m.err
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
