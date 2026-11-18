// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var loopLocalLeaseEnabled = os.Getenv("WAGO_ARM64_NO_LOOP_LOCAL_LEASE") != "1"

var temporaryLoopLeaseEnabled = os.Getenv("WAGO_ARM64_NO_TEMP_LOOP_LEASE") != "1"

var freeLoopLeaseEnabled = os.Getenv("WAGO_ARM64_NO_FREE_LOOP_LEASE") != "1"

// A lease belongs to one call-free loop. Classic leases remain flat. Temporary
// leases may cross structured control while publishing local homes only on
// branches that leave the leased region.
type loopLocalLease struct {
	depth     uint32
	count     uint8
	temporary bool
	free      bool
	local     [4]uint16
	reg       [4]Reg
	parked    [4]uint16 // displaced local index plus one
	idle      uint64
}

const loopLocalLeaseWorkLimit = 4096

func (f *fn) planLoopLocalLease(r *wasm.Reader) (loopLocalLease, bool) {
	var plan loopLocalLease
	classic := f.nLocals > 64 && f.pinnedLocalMask == 0 && f.fpinnedLocalMask == 0 && len(f.intervalLast) == 0
	free := freeLoopLeaseEnabled && !f.usesCalls && f.pinnedLocalMask != 0
	temporary := temporaryLoopLeaseEnabled && (f.usesCalls || free) && f.nLocals <= 64 && len(f.locals) == f.nLocals && f.intervalActive == 0 && f.depth() == 0
	if !loopLocalLeaseEnabled || !callFreeLoopRegionEnabled || (!classic && !temporary) || f.loopLocalLease.depth != 0 ||
		f.vectorLoopExit != nil || f.dotLoop != nil || f.byteReduceLoop != nil ||
		f.interruptible || f.threadedMemory0 || f.moduleEH || f.preserveCallerPins ||
		len(f.gcTypeLayouts) != 0 || f.gcFrameRoots != nil || len(f.customInstructions) != 0 ||
		f.inlineDepth != 0 || f.localBase != 0 ||
		f.loopLocalLeaseWork >= loopLocalLeaseWorkLimit {
		return plan, false
	}
	plan.temporary = temporary && !classic
	plan.free = plan.temporary && free
	if plan.free {
		unpinned := false
		for i, local := range f.locals {
			if local.reg == regNone && (f.localType[i] == mtI32 || f.localType[i] == mtI64) {
				unpinned = true
				break
			}
		}
		if !unpinned {
			return plan, false
		}
	}
	var cells [16]struct {
		local uint16
		refs  uint16
		reads uint16
	}
	var lowSlots [64]uint8 // cell index plus one; zero is absent
	count := 0
	depth := 0
	structured, escapes := false, false
	var mentioned uint64
	peek := *r
	start := peek.Offset()
	var imm wasm.InstructionImmediate
	for n := 0; n < 128 && peek.Offset()-start < 512; n++ {
		if f.loopLocalLeaseWork >= loopLocalLeaseWorkLimit {
			return plan, false
		}
		f.loopLocalLeaseWork++
		op, err := peek.Byte()
		if err != nil {
			return plan, false
		}
		// A final conditional self-backedge closes the candidate. Temporary
		// leases may also publish homes on conditional exits to the parent.
		// Source validation supplies the empty stack at this void loop's end.
		if op == 0x0d {
			label, err := peek.U32()
			if err != nil {
				return plan, false
			}
			if depth != 0 {
				escapes = escapes || label > uint32(depth)
				continue
			}
			if plan.temporary && label == 1 {
				escapes = true
				// The immediate enclosing void target gets current homes on its
				// taken edge. The existing cold-edge path preserves hot residency.
				if len(f.ctrl) == 0 || f.ctrl[len(f.ctrl)-1].branchArity() != 0 {
					return plan, false
				}
				continue
			}
			end, endErr := peek.Byte()
			if endErr != nil || label != 0 || end != 0x0b || peek.Offset()-start > 512 {
				return plan, false
			}
			minimumRefs := uint16(2)
			if structured {
				minimumRefs = 3
			}
			maxPins := len(plan.local)
			if plan.free {
				// Reserve only two additional pins in leaf functions. A
				// read avoided on every iteration is worth more than a store.
				minimumRefs = 4
				maxPins = 2
			}
			for slot := 0; slot < maxPins; slot++ {
				best := -1
				bestScore := uint16(0)
				for i := 0; i < count; i++ {
					if plan.temporary && f.locals[cells[i].local].reg != regNone {
						continue
					}
					score := cells[i].refs
					if plan.free {
						if cells[i].reads == 0 {
							continue
						}
						score += cells[i].reads
					}
					if score >= minimumRefs && score > bestScore {
						best, bestScore = i, score
					}
				}
				if best < 0 {
					break
				}
				plan.local[plan.count] = cells[best].local
				plan.count++
				cells[best].refs, cells[best].reads = 0, 0
			}
			if plan.free && !escapes {
				plan.idle = ^mentioned
			}
			return plan, plan.count != 0
		}
		// Structured, call-free control does not invalidate a temporary lease.
		// Track nesting so only the outer loop latch closes this candidate; inner
		// branches continue to use the same local-to-register mapping.
		switch op {
		case 0x02, 0x03, 0x04: // block, loop, if
			if !plan.temporary {
				return plan, false
			}
			structured = true
			// The module is already validated. Only advance over the signed
			// block-type LEB; the planner does not need its decoded signature.
			if _, err := peek.I64(); err != nil {
				return plan, false
			}
			depth++
			continue
		case 0x05: // else
			if !plan.temporary || depth == 0 {
				return plan, false
			}
			continue
		case 0x0b: // nested end; the outer end is consumed with its latch above
			if !plan.temporary || depth == 0 {
				return plan, false
			}
			depth--
			continue
		case 0x0c: // br
			label, err := peek.U32()
			if err != nil || !plan.temporary || depth == 0 {
				return plan, false
			}
			escapes = escapes || label > uint32(depth)
			continue
		}
		// These admitted operations have no immediates and neither access
		// memory nor name locals. The full classifier is unnecessary here.
		if op == 0x01 || op == 0x1a || op == 0x1b || op >= 0x45 && op <= 0x5a ||
			op >= 0x67 && op <= 0x8a || op == 0xa7 || op == 0xac || op == 0xad || op >= 0xc0 && op <= 0xc4 {
			continue
		}
		var x uint32
		switch {
		case op >= 0x20 && op <= 0x22:
			x, err = peek.U32()
		case op == 0x41:
			_, err = peek.I32()
		case op == 0x42:
			_, err = peek.I64()
		case op == 0x28 || op == 0x29 || op >= 0x2c && op <= 0x37 || op >= 0x3a && op <= 0x3e:
			if align, ok := peek.Peek(); ok && align < 64 {
				// An implicit index always selects memory zero. The shared
				// skipper still handles its 32- or 64-bit offset correctly.
				err = f.classifier.SkipScalarMemoryImmediate(&peek)
			} else {
				err = f.classifier.ClassifyInto(&peek, op, &imm)
				if err == nil && imm.MemIndex != 0 {
					return plan, false
				}
			}
		default:
			return plan, false
		}
		if err != nil {
			return plan, false
		}
		if op >= 0x20 && op <= 0x22 {
			if uint64(x) >= uint64(len(f.localType)) || x > 65535 || (f.localType[x] != mtI32 && f.localType[x] != mtI64) {
				return plan, false
			}
			i := count
			if x < uint32(len(lowSlots)) {
				if slot := lowSlots[x]; slot != 0 {
					i = int(slot - 1)
				}
			} else {
				i = 0
				for i < count && cells[i].local != uint16(x) {
					i++
				}
			}
			if i == count {
				if count == len(cells) {
					return plan, false
				}
				cells[i].local = uint16(x)
				if x < uint32(len(lowSlots)) {
					lowSlots[x] = uint8(i + 1)
				}
				count++
			}
			cells[i].refs++
			if plan.free {
				mentioned |= uint64(1) << x
				if op == 0x20 {
					cells[i].reads++
				}
			}
		}
	}
	return plan, false
}

func (f *fn) beginLoopLocalLease(r *wasm.Reader, fr *ctrlFrame) {
	if fr.paramN != 0 || fr.resultN != 0 {
		return
	}
	plan, ok := f.planLoopLocalLease(r)
	if !ok {
		return
	}
	// Reserve through the existing allocator masks; never displace a live
	// operand, immutable constant, memory base, global, or other reservation.
	n := 0
	registers := [...]Reg{X19, X20, X21, X22, X9, X10, X11, X12, X13, X14, X15}
	limit := 4
	freeSlots := int(plan.count)
	if plan.temporary {
		limit = len(registers)
		free := 0
		for _, r := range gpAlloc {
			if !f.pinned.has(r) && !f.reserved.has(r) && !f.pinnedLocalMask.has(r) && f.regUser[r] == nil {
				free++
			}
		}
		freeSlots = min(freeSlots, max(0, free-7))
	}
	for _, reg := range registers[:limit] {
		if n == freeSlots {
			break
		}
		if f.pinned.has(reg) || f.reserved.has(reg) || f.pinnedLocalMask.has(reg) || f.regUser[reg] != nil {
			continue
		}
		plan.reg[n] = reg
		n++
	}
	// When free registers would cut into the working set, reuse an idle
	// whole-function pin. The bounded scan proved no access to its local and
	// no edge around the lexical exit that restores it.
	for x := 0; n < int(plan.count) && plan.idle != 0 && x < f.nLocals; x++ {
		if plan.idle&(uint64(1)<<x) == 0 {
			continue
		}
		l := &f.locals[x]
		if l.isFloat || l.reg == regNone || l.state == lsConstZero ||
			f.pinned.has(l.reg) || f.reserved.has(l.reg) || f.regUser[l.reg] != nil {
			continue
		}
		plan.reg[n], plan.parked[n] = l.reg, uint16(x+1)
		n++
	}
	if n == 0 {
		return
	}
	plan.count = uint8(n)
	plan.depth = uint32(len(f.ctrl) + 1)
	for i := 0; i < n; i++ {
		x, reg := int(plan.local[i]), plan.reg[i]
		if parked := plan.parked[i]; parked != 0 {
			old := int(parked) - 1
			if f.locals[old].state == lsReg {
				f.storeLocalReg(old, reg, false)
			}
			f.locals[old].reg = regNone
			f.locals[old].state = lsMem
			f.stats.peep("loop-local-pin-exchange")
		}
		zero := f.locals[x].state == lsConstZero
		f.locals[x].reg = reg
		f.pinnedLocalMask = f.pinnedLocalMask.add(reg)
		if zero {
			f.materializeZeroLocal(x, false)
		} else {
			f.loadLocalReg(x, reg, false)
		}
		f.locals[x].state = lsReg
	}
	f.loopLocalLease = plan
	fr.set(ctrlLoopCallFree, true)
	if plan.free {
		f.stats.peep("free-loop-local-lease")
	} else if plan.temporary {
		f.stats.peep("temporary-loop-local-lease")
	} else {
		f.stats.peep("loop-local-lease")
	}
}

func (f *fn) finishLoopLocalLease(depth int) {
	plan := f.loopLocalLease
	if plan.depth != uint32(depth) {
		return
	}
	// opEnd has flushed pending roots on normal fallthrough. Early taken
	// exits already published these homes through branch convergence.
	for i := 0; i < int(plan.count); i++ {
		x, reg := int(plan.local[i]), plan.reg[i]
		f.storeLocalReg(x, reg, false)
		f.locals[x].reg = regNone
		f.locals[x].state = lsMem
		if parked := plan.parked[i]; parked != 0 {
			old := int(parked) - 1
			f.locals[old].reg = reg
			f.loadLocalReg(old, reg, false)
			f.locals[old].state = lsStackReg
		} else {
			f.pinnedLocalMask = f.pinnedLocalMask.remove(reg)
		}
	}
	f.loopLocalLease = loopLocalLease{}
}

// Temporary loop pins have source homes at every exit to an enclosing frame.
// Local mappings are restored when the lexical loop ends. Emission through the
// ordinary branch convergence lets conditional exits move these stores into
// their existing cold edges, without changing the fallthrough register values.
func (f *fn) publishTemporaryLoopExit(fr *ctrlFrame) {
	p := f.loopLocalLease
	if !p.temporary || p.depth == 0 || int(p.depth) > len(f.ctrl) {
		return
	}
	// The lease remains valid for the loop itself and every nested target.
	// Publish only when the branch actually leaves the leased region.
	for i := int(p.depth) - 1; i < len(f.ctrl); i++ {
		if fr == &f.ctrl[i] {
			return
		}
	}
	for i := 0; i < int(p.count); i++ {
		x := int(p.local[i])
		f.storeLocalReg(x, p.reg[i], false)
		// Both locations now hold the source value. Ordinary convergence can
		// reuse this publication; a cold edge restores the hot state snapshot.
		f.locals[x].state = lsStackReg
	}
}
