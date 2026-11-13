// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

var loopLocalLeaseEnabled = os.Getenv("WAGO_ARM64_NO_LOOP_LOCAL_LEASE") != "1"

var temporaryLoopLeaseEnabled = os.Getenv("WAGO_ARM64_NO_TEMP_LOOP_LEASE") != "1"

// A lease belongs to one flat loop. No helper or internal control join can
// run while its locals use the ordinary pinned-local machinery. Temporary
// leases also permit conditional exits to the immediate enclosing void frame.
type loopLocalLease struct {
	depth     uint32
	count     uint8
	temporary bool
	local     [4]uint16
	reg       [4]Reg
}

const loopLocalLeaseWorkLimit = 4096

func (f *fn) planLoopLocalLease(r *wasm.Reader) (loopLocalLease, bool) {
	var plan loopLocalLease
	classic := f.nLocals > 64 && f.pinnedLocalMask == 0 && f.fpinnedLocalMask == 0 && len(f.intervalLast) == 0
	temporary := temporaryLoopLeaseEnabled && f.usesCalls && f.nLocals <= 64 && len(f.locals) == f.nLocals && f.intervalActive == 0 && f.depth() == 0
	if !loopLocalLeaseEnabled || !callFreeLoopRegionEnabled || (!classic && !temporary) || f.loopLocalLease.depth != 0 ||
		f.pureReduce != nil || f.dotLoop != nil || f.byteReduceLoop != nil ||
		f.interruptible || f.threadedMemory0 || f.moduleEH || f.preserveCallerPins ||
		len(f.gcTypeLayouts) != 0 || f.gcFrameRoots != nil || len(f.customInstructions) != 0 ||
		f.inlineDepth != 0 || f.localBase != 0 ||
		f.loopLocalLeaseWork >= loopLocalLeaseWorkLimit {
		return plan, false
	}
	plan.temporary = temporary && !classic
	var cells [16]struct {
		local uint16
		refs  uint16
	}
	count := 0
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
			if plan.temporary && label == 1 {
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
			for slot := 0; slot < len(plan.local); slot++ {
				best := -1
				for i := 0; i < count; i++ {
					if plan.temporary && f.locals[cells[i].local].reg != regNone {
						continue
					}
					if cells[i].refs >= 2 && (best < 0 || cells[i].refs > cells[best].refs) {
						best = i
					}
				}
				if best < 0 {
					break
				}
				plan.local[plan.count] = cells[best].local
				plan.count++
				cells[best].refs = 0
			}
			return plan, plan.count != 0
		}
		// These admitted operations have no immediates and neither access
		// memory nor name locals. The full classifier is unnecessary here.
		if op == 0x01 || op == 0x1a || op == 0x1b || op >= 0x45 && op <= 0x5a ||
			op >= 0x67 && op <= 0x8a || op == 0xa7 || op == 0xac || op == 0xad || op >= 0xc0 && op <= 0xc4 {
			continue
		}
		if !(op >= 0x20 && op <= 0x22 ||
			op == 0x28 || op == 0x29 || op >= 0x2c && op <= 0x37 || op >= 0x3a && op <= 0x3e ||
			op == 0x41 || op == 0x42) ||
			f.classifier.ClassifyInto(&peek, op, &imm) != nil {
			return plan, false
		}
		if imm.TouchesMemory && imm.MemIndex != 0 {
			return plan, false
		}
		if op >= 0x20 && op <= 0x22 {
			x := imm.Index
			if uint64(x) >= uint64(len(f.localType)) || x > 65535 || (f.localType[x] != mtI32 && f.localType[x] != mtI64) {
				return plan, false
			}
			i := 0
			for i < count && cells[i].local != uint16(x) {
				i++
			}
			if i == count {
				if count == len(cells) {
					return plan, false
				}
				cells[i].local = uint16(x)
				count++
			}
			cells[i].refs++
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
	if plan.temporary {
		limit = len(registers)
		free := 0
		for _, r := range gpAlloc {
			if !f.pinned.has(r) && !f.reserved.has(r) && !f.pinnedLocalMask.has(r) && f.regUser[r] == nil {
				free++
			}
		}
		if free <= 7 {
			return
		}
		if int(plan.count) > free-7 {
			plan.count = uint8(free - 7)
		}
	}
	for _, reg := range registers[:limit] {
		if f.pinned.has(reg) || f.reserved.has(reg) || f.pinnedLocalMask.has(reg) || f.regUser[reg] != nil {
			continue
		}
		plan.reg[n] = reg
		n++
		if n == int(plan.count) {
			break
		}
	}
	if n == 0 {
		return
	}
	plan.count = uint8(n)
	plan.depth = uint32(len(f.ctrl) + 1)
	for i := 0; i < n; i++ {
		x, reg := int(plan.local[i]), plan.reg[i]
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
	if plan.temporary {
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
		f.pinnedLocalMask = f.pinnedLocalMask.remove(reg)
	}
	f.loopLocalLease = loopLocalLease{}
}

// Temporary loop pins have source homes at every exit to an enclosing frame.
// Local mappings are restored when the lexical loop ends. Emission through the
// ordinary branch convergence lets conditional exits move these stores into
// their existing cold edges, without changing the fallthrough register values.
func (f *fn) publishTemporaryLoopExit(fr *ctrlFrame) {
	p := f.loopLocalLease
	if !p.temporary || p.depth == 0 || int(p.depth) > len(f.ctrl) || fr == &f.ctrl[p.depth-1] {
		return
	}
	for i := 0; i < int(p.count); i++ {
		x := int(p.local[i])
		f.storeLocalReg(x, p.reg[i], false)
		// Both locations now hold the source value. Ordinary convergence can
		// reuse this publication; a cold edge restores the hot state snapshot.
		f.locals[x].state = lsStackReg
	}
}
