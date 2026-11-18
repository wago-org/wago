// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

// A/B switch for qualification; supported recipes are admitted by default.
var integerMapLoopEnabled = os.Getenv("WAGO_AMD64_NO_INTEGER_MAP_LOOP") != "1"
var integerMapPackEnabled = true
var integerMapTestFast bool
var integerMapInvariantReuse = os.Getenv("WAGO_AMD64_NO_INTEGER_MAP_INVARIANT_REUSE") != "1"
var integerMapWideInterleave = os.Getenv("WAGO_AMD64_NO_INTEGER_MAP_WIDE_INTERLEAVE") != "1"
var integerMapIncrementCache = os.Getenv("WAGO_AMD64_NO_INTEGER_MAP_INCREMENT_CACHE") != "1"
var integerMapAddressCache = os.Getenv("WAGO_AMD64_NO_INTEGER_MAP_ADDRESS_CACHE") != "1"
var integerMapAffineExit = os.Getenv("WAGO_AMD64_NO_INTEGER_MAP_AFFINE_EXIT") != "1"
var integerMapSLPEnabled = os.Getenv("WAGO_AMD64_NO_INTEGER_MAP_SLP") != "1"
var integerMapSLPWide = os.Getenv("WAGO_AMD64_NO_INTEGER_MAP_SLP_WIDE") != "1"
var integerMapWideUnroll = os.Getenv("WAGO_AMD64_NO_WIDE_UNROLL") != "1"
var integerMapWideInvariantCache = os.Getenv("WAGO_AMD64_NO_WIDE_INVARIANT_CACHE") != "1"

const integerMapFunctionWorkLimit = 256

// A cheap, bounded admission screen keeps the candidate workspace and detailed
// analysis away from ordinary loops. All failed screens consume the same budget.
func (f *fn) integerMapScreen(source wasm.Reader) bool {
	if f.integerMapWork >= integerMapFunctionWorkLimit-integerMapMaxNodes {
		return false
	}
	bt, err := source.Byte()
	if err != nil || bt != 0x40 {
		return false
	}
	for i := 0; i < 24; i++ {
		f.integerMapWork++
		op, err := source.Byte()
		if err != nil {
			return false
		}
		switch {
		case op == 0x28:
			return f.integerMapAfterLoad(source)
		case op == 0x20 || op == 0x21 || op == 0x22:
			if _, err := source.U32(); err != nil {
				return false
			}
		case op == 0x41:
			if _, err := source.I32(); err != nil {
				return false
			}
		case op == 0x01 || op == 0x1a || op == 0x1b || op >= 0x45 && op <= 0x4f || op >= 0x6a && op <= 0x6c || op >= 0x71 && op <= 0x76:
		default:
			return false
		}
	}
	return false
}

// Reject unsupported control and memory forms shortly after the first load,
// before constructing the larger candidate workspace. Reaching the probe
// limit retains eligibility; the full planner still decides legality.
func (f *fn) integerMapAfterLoad(source wasm.Reader) bool {
	index, _, err := f.readMemArg(&source)
	if err != nil || index != 0 {
		return false
	}
	for i := 0; i < 12; i++ {
		if f.integerMapWork >= integerMapFunctionWorkLimit-integerMapMaxNodes {
			return false
		}
		f.integerMapWork++
		op, err := source.Byte()
		if err != nil {
			return false
		}
		switch {
		case op == 0x36:
			return true
		case op == 0x28:
			index, _, err = f.readMemArg(&source)
			if err != nil || index != 0 {
				return false
			}
		case op == 0x20 || op == 0x21 || op == 0x22:
			if _, err = source.U32(); err != nil {
				return false
			}
		case op == 0x41:
			if _, err = source.I32(); err != nil {
				return false
			}
		case op == 0x01 || op == 0x1a || op == 0x1b || op >= 0x45 && op <= 0x4f || op >= 0x6a && op <= 0x6c || op >= 0x71 && op <= 0x76:
		default:
			return false
		}
	}
	return true
}

func (f *fn) tryIntegerMapLoop(r *wasm.Reader) (bool, error) {
	if f.unreachable {
		f.stats.peep("integer-map-unreachable-reject")
		return false, nil
	}
	if f.depth() > 4 {
		f.stats.peep("integer-map-stack-reject")
		return false, nil
	}
	if f.moduleEH || f.localBase != 0 || f.interruptible || f.gcFrameRoots != nil || len(f.customInstructions) != 0 || f.nLocals > 256 || f.m.MemCount() == 0 || f.memoryAddr64(0) || f.threadedMemory0 || profileEnabled {
		f.stats.peep("integer-map-context-reject")
		return false, nil
	}
	if f.memSizeReg == regNone {
		f.stats.peep("integer-map-memory-size-reject")
		return false, nil
	}
	if len(f.intervalReg) != 0 {
		f.stats.peep("integer-map-interval-reject")
		return false, nil
	}
	features := shared.AMD64ModernBaseline | shared.AMD64BMI2
	if f.sc != nil {
		features = f.sc.amd64Features
	}
	if !features.Has(shared.AMD64SSE41) || !f.integerMapScreen(*r) {
		f.stats.peep("integer-map-screen-reject")
		return false, nil
	}
	// Accepted recipes store a word in the bounded window. Raw byte screening
	// can have false positives in immediates, but not false negatives for a
	// store within the bounded planner window.
	look := *r
	size := look.BytesLeft()
	if size > integerMapMaxBytes+1 {
		size = integerMapMaxBytes + 1
	}
	window, _ := look.Bytes(size)
	if bytes.IndexByte(window, 0x36) < 0 {
		return false, nil
	}
	return f.tryIntegerMapCandidate(r)
}

// Keep the large bounded workspace off the ordinary loop-screen path.
//
//go:noinline
func (f *fn) tryIntegerMapCandidate(r *wasm.Reader) (bool, error) {
	look := *r
	if _, err := look.Byte(); err != nil {
		return false, nil
	}
	// Charge the complete bounded probe even if decoding rejects early.
	f.integerMapWork += integerMapMaxNodes
	var p integerMapPlan
	if !inspectIntegerMapLoop(look, f.localType, f.classifier, &p) {
		f.stats.peep("integer-map-capture-reject")
		return false, nil
	}
	facts := p.affineFacts()
	ind, ok := p.induction(&facts)
	if !ok {
		f.stats.peep("integer-map-induction-reject")
		return false, nil
	}
	used := p.requiredNodes()
	guards, ok := p.guardPlan(&facts, ind, &used)
	if !ok {
		f.stats.peep("integer-map-guard-reject")
		return false, nil
	}
	recipe, ok := p.buildIntegerMapRecipe(ind, &guards.memory, &facts)
	if ok && guards.packStores {
		if recipe.registers <= 13 {
			recipe.registers += 3
		} else {
			guards.packStores = false
		}
	}
	if !ok || !integerMapBodySupported(&p, &guards, &recipe) {
		f.stats.peep("integer-map-recipe-reject")
		return false, nil
	}
	if guards.packStores {
		f.stats.peep("integer-map-packed-stores")
	}
	f.stats.peep("integer-map-candidate")
	if recipe.strided && (!guards.packStores || !integerMapWideInterleave) {
		guards.lanes = 2
	}
	var slp integerMapSLP
	var hasSLP bool
	if integerMapSLPEnabled && guards.packStores && guards.lanes == 4 {
		slp, hasSLP = buildIntegerMapSLP(&p, &guards, &recipe, ind)
	}
	guards.minTrip = 16
	prefixDepth := f.depth()
	depth := len(f.ctrl)
	if err := f.opBlockPlain(r, 0x03); err != nil {
		return true, err
	}
	pinExchange := f.ctrl[len(f.ctrl)-1].has(ctrlLoopPinExchange)
	var gp [4]Reg
	n := 0
	blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
	for _, reg := range gpAlloc {
		if !blocked.has(reg) && f.regUser[reg] == nil {
			gp[n] = reg
			n++
			if n == len(gp) {
				break
			}
		}
	}
	if n != len(gp) {
		f.stats.peep("integer-map-gp-reject")
		return true, nil
	}
	var entry [256]regionEntryLocal
	for i := 0; i < f.nLocals; i++ {
		entry[i] = regionEntryLocal{f.locals[i].reg, f.locals[i].state}
	}
	entryF := f.fpinned.union(f.fpinnedLocalMask).union(f.fconstMask()).union(f.v128ConstMask())
	reserved, pinned, memSize, memLease := f.reserved, f.pinned, f.memSizeReg, f.memSizeRegionalLease
	slots := int(p.localN) + 1 + 2*int(guards.memory.streamN) + int(p.localN)
	slot := f.allocSpillSlots(slots)
	slotBase := f.spillOff(slot)
	entryDispatch := f.a.JmpPlaceholder()
	capture := f.a.Len()
	for i, l := range p.locals[:p.localN] {
		d := f.locals[l.index]
		switch {
		case d.state == lsConstZero:
			f.a.Xor32(gp[0], gp[0])
		case d.reg != regNone && d.state != lsMem:
			f.a.MovRegReg32(gp[0], d.reg)
		default:
			f.loadFrameInt(gp[0], f.localAddr(int(l.index)), mtI32)
		}
		f.a.Store64(RSP, slotBase+int32(i*8), gp[0])
	}
	ge := integerMapGuardEmitter{a: f.a, plan: guards, gp: gp, memorySize: memSize, slotBase: slotBase}
	ge.emit()
	if hasSLP && ge.failureN+int(slp.relationN) > len(ge.failures) {
		hasSLP = false
	}
	if hasSLP {
		for _, rel := range slp.relations[:slp.relationN] {
			f.a.Load64(gp[0], RSP, ge.slot(ge.startSlot(int(rel.a))))
			f.a.Load64(gp[1], RSP, ge.slot(ge.startSlot(int(rel.b))))
			f.a.AluRI(0, gp[1], 4, true)
			f.a.Cmp64(gp[0], gp[1])
			ge.fail(condNE)
		}
	}
	toFast := f.a.JmpPlaceholder()
	f.a.AlignLoop()
	fallback := f.a.Len()
	ge.patchFallback(fallback)
	if integerMapTestFast {
		f.trapAlways(trapUnreachable)
	}
	f.ctrl[len(f.ctrl)-1].controlSite = f.a.Len()
	f.integerMapActive = true
	err := f.bodyLoop(r, depth)
	f.integerMapActive = false
	if err != nil {
		return true, err
	}
	toDone := f.a.JmpPlaceholder()
	f.a.PatchRel32(toFast, f.a.Len())
	valid := !f.unreachable && f.depth() == prefixDepth && f.s.canonicalSlots && f.reserved == reserved && f.pinned == pinned && f.memSizeReg == memSize && f.memSizeRegionalLease == memLease
	var written [256]bool
	for _, l := range p.locals[:p.localN] {
		if l.written {
			written[l.index] = true
		}
	}
	var reload [256]bool
	for i := 0; i < f.nLocals; i++ {
		old, current := entry[i], f.locals[i]
		if old.reg == current.reg {
			if !written[i] && old.state != current.state {
				valid = false
			}
			continue
		}
		if !pinExchange {
			valid = false
			continue
		}
		if written[i] {
			continue
		} // the fast path publishes this final local
		switch {
		case old.reg == regNone && old.state == lsMem && current.reg != regNone && current.state != lsMem && !current.isFloat:
			// The exchanged-out owner was homed before the loop. Restore its
			// final register on the fast path, which skips scalar loop exit.
			reload[i] = true
		case old.reg != regNone && old.state == lsStackReg && current.reg == regNone && current.state == lsMem:
			// An unchanged exchanged-in local still has its original home.
		default:
			valid = false
		}
	}
	for _, reg := range gp {
		if f.regUser[reg] != nil || f.pinnedLocalMask.has(reg) {
			valid = false
		}
	}
	mask := entryF.union(f.fpinned).union(f.fpinnedLocalMask).union(f.fconstMask()).union(f.v128ConstMask())
	var fp [16]Reg
	n = 0
	wantFP := int(recipe.registers)
	if integerMapIncrementCache && recipe.used[p.locals[ind.local].entry] && wantFP < len(fp) {
		wantFP++
	}
	for reg := Reg(0); reg < 16; reg++ {
		if !mask.has(reg) && f.fregUser[reg] == nil && n < wantFP {
			fp[n] = reg
			n++
		}
	}
	if n < int(recipe.registers) {
		valid = false
	}
	if valid {
		f.a.PatchRel32(entryDispatch, capture)
		f.cpuHas(shared.AMD64SSE41)
		outputBase := slotBase + int32((int(p.localN)+1+2*int(guards.memory.streamN))*8)
		increment := regNone
		if n > int(recipe.registers) {
			increment = fp[recipe.registers]
		}
		be := integerMapBodyEmitter{a: f.a, plan: &p, guard: &ge, recipe: &recipe, facts: &facts, induction: ind, gp: gp, fp: fp, increment: increment, hasIncrement: increment != regNone, memoryBase: RBX, outputBase: outputBase}
		if hasSLP {
			be.slp = &slp
			f.stats.peep("integer-map-slp")
			if integerMapSLPWide && f.cpuHas(shared.AMD64AVX|shared.AMD64AVX2) {
				be.slpWide = true
				f.stats.peep("integer-map-slp-wide")
			}
		}
		if integerMapAddressCache {
			blocked := f.pinned.union(f.pinnedLocalMask).union(f.reserved)
			for _, reg := range gp {
				blocked = blocked.add(reg)
			}
			for i, stream := range guards.memory.streams[:guards.memory.streamN] {
				base, _ := shareStream(&guards.memory, i)
				if stream.stride == 0 || base != i || be.addressN == uint8(len(be.addressReg)) {
					continue
				}
				for _, reg := range gpAlloc {
					if !blocked.has(reg) && f.regUser[reg] == nil {
						be.addressBase[be.addressN], be.addressReg[be.addressN] = base, reg
						be.addressN++
						blocked = blocked.add(reg)
						break
					}
				}
			}
		}
		be.emit()
		if integerMapWideUnroll && be.slpWide {
			f.stats.peep("integer-map-slp-wide-unroll")
		}
		if be.wideInvariant {
			f.stats.peep("integer-map-wide-invariant-cache")
		}
		for i, needed := range reload[:f.nLocals] {
			if needed {
				f.loadLocalReg(i, f.locals[i].reg, false)
			}
		}
		for i, l := range p.locals[:p.localN] {
			if !l.written {
				continue
			}
			d := f.locals[l.index]
			if d.state == lsConstZero {
				continue
			}
			f.a.Load32(gp[0], RSP, outputBase+int32(i*8))
			if d.reg == regNone || d.state != lsReg {
				f.a.Store32(RSP, f.localAddr(int(l.index)), gp[0])
			}
			if d.reg != regNone && d.state != lsMem {
				f.a.MovRegReg32(d.reg, gp[0])
			}
		}
		f.stats.peep("integer-map-loop")
	} else {
		f.a.PatchRel32(entryDispatch, fallback)
		f.a.JmpBack(fallback)
		f.stats.peep("integer-map-exit-reject")
	}
	f.a.PatchRel32(toDone, f.a.Len())
	return true, nil
}
