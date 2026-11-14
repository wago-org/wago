// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

// gcResolvedObject is a transient raw-address certificate. Unlike collector
// roots, it never survives a call, allocation, or unknown effect. A bounded,
// effect-free if may retain an unchanged entry certificate.
type gcResolvedObject struct {
	valid         bool
	local         int
	typeIndex     uint32
	requiredBytes uint32
	reg           Reg
	spill         bool
	spillSlot     int
	previousFloor int
	inputTop      bool
}

// prepareGCResolvedObject is the central invalidation gate. Apart from proven
// pure void ifs, only
// noncollecting integer operations, local reads, and assignments to other
// locals retain the transient address. Assigning its source local invalidates
// it in setLocal. Calls, other control boundaries, and unknown effects drop it.
func (f *fn) prepareGCResolvedObject(op byte) {
	if !f.gcResolved.valid {
		return
	}
	if f.gcResolved.inputTop && op != 0xfb {
		f.gcResolved.inputTop = false
	}
	if f.gcPureIfEndPC != 0 && f.wasmPC > f.gcPureIfEndPC {
		f.gcPureIfEndPC = 0
	}
	if f.gcPureIfEndPC != 0 && f.wasmPC <= f.gcPureIfEndPC && (op == 0x04 || op == 0x05 || op == 0x0b) {
		return
	}
	switch op {
	case 0x1a, 0x20, 0x21, 0x22, 0x41, 0x42, 0x43, 0x44, 0xfb,
		0x45, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f,
		0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a,
		0x6a, 0x6b, 0x6c, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
		0x7c, 0x7d, 0x7e, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89, 0x8a:
		return
	default:
		f.invalidateGCResolvedObject()
	}
}

func (f *fn) prepareGCResolvedFB(sub uint32) {
	if !f.gcResolved.valid {
		return
	}
	if f.gcResolved.inputTop && sub != 2 && sub != 3 && sub != 4 {
		f.gcResolved.inputTop = false
	}
	switch sub {
	case 2, 3, 4, 5, 11, 12, 13, 14, 15:
		return
	default:
		f.invalidateGCResolvedObject()
	}
}

func (f *fn) invalidateGCResolvedObject() {
	f.gcPureIfEndPC = 0
	if !f.gcResolved.valid {
		return
	}
	if f.gcResolved.spill {
		end := f.gcResolved.spillSlot + 1
		if f.spillFloor != end {
			panic("amd64: GC resolved-address spill invalidated under another spill-floor owner")
		}
		f.spillFloor = f.gcResolved.previousFloor
	} else {
		f.pinned = f.pinned.remove(f.gcResolved.reg)
	}
	f.gcResolved = gcResolvedObject{}
}

// cacheGCResolvedObjectInSpill retains a raw address in one compiler-frame
// slot when register pressure blocks the preferred register certificate. The
// caller supplies the floor that preceded its temporary operand staging so
// invalidation can release the complete reservation.
func (f *fn) cacheGCResolvedObjectInSpill(local int, typeIndex, requiredBytes uint32, obj Reg, previousFloor int) {
	slot := f.allocSpillSlot()
	f.a.Store64(RSP, f.spillOff(slot), obj)
	f.spillFloor = slot + 1
	f.gcResolved = gcResolvedObject{
		valid:         true,
		local:         local,
		typeIndex:     typeIndex,
		requiredBytes: requiredBytes,
		reg:           regNone,
		spill:         true,
		spillSlot:     slot,
		previousFloor: previousFloor,
	}
}

// materializeGCResolvedObject returns the cached raw address for one field
// access. Spill-backed certificates borrow and pin a temporary register only
// for that access; the frame slot remains the certificate owner.
func (f *fn) materializeGCResolvedObject() (Reg, func()) {
	if !f.gcResolved.spill {
		return f.gcResolved.reg, func() {}
	}
	r := f.allocReg(0)
	f.a.Load64(r, RSP, f.spillOff(f.gcResolved.spillSlot))
	f.pinned = f.pinned.add(r)
	return r, func() {
		f.pinned = f.pinned.remove(r)
		f.release(r)
	}
}

func (f *fn) gcResolvedRegister() Reg {
	block := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	for _, reg := range [...]Reg{RBP, R12, R13, R14} {
		if f.regUser[reg] == nil && !block.has(reg) {
			return reg
		}
	}
	return regNone
}

// Borrowed reads and owned snapshots can identify the current local value.
// Assignment must clear the marker on outstanding owned snapshots.
func gcLocalProvenance(e *elem) (int, bool) {
	if e == nil || !e.isValue() {
		return 0, false
	}
	switch e.st.kind {
	case stLocalRef, stLocalReg:
		return e.st.index(), true
	case stReg:
		// Materializing a local or a proved identity cast keeps the exact value
		// but turns its storage into an owned register. slot=local+1 is the
		// explicit provenance marker; ordinary computed registers keep slot zero.
		if e.st.slot != 0 {
			return int(e.st.slot - 1), true
		}
	}
	return 0, false
}

func markGCLocalProvenance(e *elem, local int) {
	if e != nil && e.isValue() && e.st.kind == stReg && local >= 0 {
		e.st.slot = uint32(local + 1)
	}
}

func (f *fn) invalidateGCLoadFactsForLocal(local int) {
	if f.gcResolved.valid && f.gcResolved.local == local {
		f.invalidateGCResolvedObject()
	}
	if !f.gcStructHelpers {
		return
	}
	// Collector references use i64 storage. Numeric locals of another width
	// cannot own an object-address certificate.
	if uint(local) < uint(len(f.localType)) && f.localType[local] != mtI64 {
		return
	}
	// Owned values retain the old value after assignment, but no longer name
	// the current local. Clear their identity even without a cached address or
	// a pending borrowed local read. The register-owner table has fixed size.
	marker := uint32(local + 1)
	for _, e := range f.regUser[:] {
		if e != nil && e.st.kind == stReg && e.st.slot == marker {
			e.st.slot = 0
		}
	}
}

func (f *fn) invalidateGCMutableLoadFacts() {
	f.invalidateGCResolvedObject()
	f.invalidateGCFreshStructFact()
}
