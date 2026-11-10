// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

var scalarLoopUnrollEnabled = true

// scalarLoopBody admits only position-independent integer register operations.
// This deliberately small decoder recognizes complete encoder forms, including
// their immediates, so an immediate byte can never masquerade as an instruction.
// Memory accesses, branches, calls, traps and all unrecognized prefixes fall back.
func scalarLoopBody(b []byte) bool { return scalarLoopBodyWithPins(b, 0) }

func scalarLoopBodyWithPins(b []byte, pins regMask) bool {
	_, ok := scalarLoopBodyWrites(b, pins)
	return ok
}

func scalarLoopBodyWrites(b []byte, pins regMask) (regMask, bool) {
	_, writes, ok := scalarLoopBodyRegisters(b, pins)
	return writes, ok
}

// Register reads include every LEA base/index and both operands of destructive
// arithmetic. None of the admitted instructions consumes incoming flags.
func scalarLoopBodyRegisters(b []byte, pins regMask) (regMask, regMask, bool) {
	reads, writes, _, ok := scalarLoopBodyEffects(b, pins)
	return reads, writes, ok
}

func scalarLoopBodyEffects(b []byte, pins regMask) (regMask, regMask, bool, bool) {
	return scalarLoopBodyEffectsWithSelects(b, pins, nil)
}

func scalarLoopBodyEffectsWithSelects(b []byte, pins regMask, selects *[]scalarLoopSelect) (regMask, regMask, bool, bool) {
	definesFlags, compareFlags := false, false
	var reads, writes, clean32, constantSteps, otherWrites regMask
	periodicTest := regNone
	firstSelect := 0
	if selects != nil {
		firstSelect = len(*selects)
	}
	if len(b) == 0 || len(b) > 48 {
		return 0, 0, false, false
	}
	for p := 0; p < len(b); {
		start := p
		rex := byte(0)
		if b[p]&0xf0 == 0x40 {
			rex = b[p]
			p++
			if p == len(b) {
				return 0, 0, false, false
			}
		}
		op := b[p]
		p++
		if op == 0x0f {
			if selects == nil || p == len(b) || b[p] < 0x40 || b[p] > 0x4f || !compareFlags {
				return 0, 0, false, false
			}
			p++
		}
		if p == len(b) {
			return 0, 0, false, false
		}
		m := b[p]
		p++
		mod, r, rm := m>>6, Reg((m>>3)&7)|Reg((rex>>2)&1)<<3, Reg(m&7)|Reg(rex&1)<<3
		dst := rm
		imm := 0
		switch op {
		case 0x0f: // validated register CMOVcc; reads its old destination too.
			if mod != 3 {
				return 0, 0, false, false
			}
			dst = r
			reads = reads.add(r).add(rm)
			*selects = append(*selects, scalarLoopSelect{at: start, zero: rex&8 == 0 && !clean32.has(dst), periodicReg: periodicTest})
		case 0x01, 0x09, 0x21, 0x29, 0x31, 0x89: // register destination r/m
			if mod != 3 {
				return 0, 0, false, false
			}
			reads = reads.add(r)
			if op != 0x89 {
				reads = reads.add(rm)
			}
		case 0x03, 0x0b, 0x23, 0x2b, 0x33, 0x8b: // register destination reg
			if mod != 3 {
				return 0, 0, false, false
			}
			dst = r
			reads = reads.add(rm)
			if op != 0x8b {
				reads = reads.add(r)
			}
		case 0x39, 0x3b, 0x85: // CMP / TEST
			if mod != 3 {
				return 0, 0, false, false
			}
			dst = regNone
			reads = reads.add(r).add(rm)
		case 0xf6, 0xf7: // register TEST immediate; private selection proof only.
			if selects == nil || mod != 3 || (m>>3)&7 != 0 || op == 0xf6 && rex == 0 && rm >= 4 {
				return 0, 0, false, false
			}
			dst = regNone
			reads = reads.add(rm)
			imm = 4
			if op == 0xf6 {
				imm = 1
			}
		case 0x81, 0x83: // integer immediate ALU, excluding ADC/SBB
			if mod != 3 || (m>>3)&7 == 2 || (m>>3)&7 == 3 {
				return 0, 0, false, false
			}
			reads = reads.add(rm)
			if (m>>3)&7 == 7 {
				dst = regNone
			}
			imm = 4
			if op == 0x83 {
				imm = 1
			}
		case 0x8d: // LEA has an address encoding but never accesses memory.
			if mod == 3 || mod == 0 && m&7 == 5 {
				return 0, 0, false, false
			}
			dst = r
			base := rm
			if m&7 == 4 {
				if p == len(b) {
					return 0, 0, false, false
				}
				sib := b[p]
				p++
				if mod == 0 && sib&7 == 5 {
					return 0, 0, false, false
				}
				base = Reg(sib&7) | Reg(rex&1)<<3
				index := Reg((sib>>3)&7) | Reg((rex>>1)&1)<<3
				if index != RSP {
					reads = reads.add(index)
				}
			}
			reads = reads.add(base)
			if mod == 1 {
				imm = 1
			}
			if mod == 2 {
				imm = 4
			}
		default:
			return 0, 0, false, false
		}
		if dst == RSP || dst == RBP && !pins.has(RBP) || len(b)-p < imm {
			return 0, 0, false, false
		}
		if dst != regNone {
			// Only constant ADD/SUB updates preserve an induction register's
			// low two bits at the same site after four original iterations.
			if (op == 0x81 || op == 0x83) && ((m>>3)&7 == 0 || (m>>3)&7 == 5) {
				constantSteps = constantSteps.add(dst)
			} else {
				otherWrites = otherWrites.add(dst)
			}
			writes = writes.add(dst)
			if rex&8 == 0 {
				clean32 = clean32.add(dst)
			} else {
				clean32 = clean32.remove(dst)
			}
		}
		if op != 0x89 && op != 0x8b && op != 0x8d && op != 0x0f {
			definesFlags = true
			compareFlags = op == 0x39 || op == 0x3b || op == 0x85 || op == 0xf6 || op == 0xf7 ||
				(op == 0x81 || op == 0x83) && (m>>3)&7 == 7
			periodicTest = regNone
			if op == 0xf6 || op == 0xf7 {
				// TEST immediate: only bits 0..1 may influence these flags.
				// F6's high-byte register aliases were already excluded above.
				low := b[p]
				small := low != 0 && low <= 3
				for j := 1; j < imm; j++ {
					small = small && b[p+j] == 0
				}
				if small {
					periodicTest = rm
				}
			}
		}
		p += imm
	}
	if selects != nil {
		for i := firstSelect; i < len(*selects); i++ {
			site := &(*selects)[i]
			site.periodic = site.periodicReg != regNone && constantSteps.has(site.periodicReg) && !otherWrites.has(site.periodicReg)
		}
	}
	return reads, writes, definesFlags, true
}

// tryScalarPosttestLoop keeps every original iteration's exit test, with three
// forward exits and one backedge per group. No induction/bound-distance proof is
// needed: zero bounds, signed comparisons and modular integer wrap keep their
// original behavior. The bounded pure-register body has no relocation inventory.
func (f *fn) tryScalarPosttestLoop(fr *ctrlFrame, fi, depth int, cc Cond) bool {
	if !f.opt(optScalarLoopUnroll) || f.interruptible || profileEnabled ||
		f.stats != nil && f.stats.RecordSources || f.moduleEH || f.gcFrameRoots != nil ||
		f.policy.CompactNative || len(f.customInstructions) != 0 ||
		fi != len(f.ctrl)-1 || fr.kind != cfLoop || fr.paramN != 0 || fr.resultN != 0 || fr.height != 0 || depth != 0 ||
		fr.controlSite < 0 || fr.controlSite >= f.a.Len() {
		return false
	}
	body := f.a.B[fr.controlSite:]
	var saved [128]byte
	n := 0
	selectBranches := false
	lazyBranches := false
	periodicBranches := false
	if scalarLoopBody(body) {
		n = copy(saved[:], body)
	} else {
		// Raw short branches must never cross a compacting finalizer rewrite.
		if f.compactNative() {
			return false
		}
		periodic := false
		if !pureSelectEnabled {
			// Unrolling alone is not branch-probability evidence. Keep the original
			// CMOVs; the same bounded decoder proves every flag read body-local.
			var storage [16]scalarLoopSelect
			selects := storage[:0]
			if _, _, _, ok := scalarLoopBodyEffectsWithSelects(body, 0, &selects); !ok || len(selects) == 0 {
				return false
			}
			periodic = true
			for _, site := range selects {
				periodic = periodic && site.periodic
			}
			if !periodic {
				n = copy(saved[:], body)
				f.stats.peep("scalar-cmov-unroll4")
			}
		}
		if pureSelectEnabled || periodic {
			// A constant-step register advances by four deltas per group, so
			// each site's low-two-bit TEST has a fixed direction across groups.
			// Other conditions require the explicit branch-conversion opt-in.
			var ok bool
			n, ok = scalarLazySelectBranchBody(body, saved[:], f.scalarLoopLiveRegisters())
			lazyBranches = ok
			if !ok {
				n, ok = scalarSelectBranchBody(body, saved[:])
				if !ok {
					return false
				}
			}
			f.a.B = append(f.a.B[:fr.controlSite], saved[:n]...)
			selectBranches = true
			periodicBranches = periodic
		}
	}
	var exits [3]int
	for i := range exits {
		exits[i] = f.a.JccPlaceholder(invertCond(cc))
		f.a.B = append(f.a.B, saved[:n]...)
	}
	back := f.a.JccPlaceholder(cc)
	f.a.PatchRel32(back, fr.controlSite)
	for _, exit := range exits {
		f.a.PatchRel32(exit, f.a.Len())
	}
	if periodicBranches {
		f.stats.peep("scalar-periodic-select-unroll4")
	}
	if lazyBranches {
		f.stats.peep("scalar-lazy-select-unroll4")
	}
	if selectBranches {
		f.stats.peep("scalar-select-branch-unroll4")
	}
	f.stats.peep("scalar-posttest-unroll4")
	return true
}

// Each copied selection gets a distinct branch predictor site. Replacing CMOV
// with a skip over MOV preserves both values and flags, while removing the
// conditional-move dependency from the selected value. Only complete register
// forms with an in-body CMP/TEST flag producer are admitted.
type scalarLoopSelect struct {
	at          int
	zero        bool
	periodicReg Reg
	periodic    bool
}

func scalarSelectBranchBody(body, out []byte) (int, bool) {
	var storage [16]scalarLoopSelect
	sites := storage[:0]
	if _, _, _, ok := scalarLoopBodyEffectsWithSelects(body, 0, &sites); !ok || len(sites) == 0 {
		return 0, false
	}
	needed := len(body) + len(sites)
	for _, site := range sites {
		if site.zero {
			needed += 3 // conservative maximum for MOV r32,r32
		}
	}
	if needed > len(out) {
		return 0, false
	}
	n, previous := 0, 0
	for _, site := range sites {
		at := site.at
		n += copy(out[n:], body[previous:at])
		op := at
		if body[op]&0xf0 == 0x40 {
			op++
		}
		if site.zero {
			// CMOV r32 clears the upper half even when its condition is false.
			// A skipped MOV must retain that effect for an incoming dirty carrier.
			dst := Reg((body[op+2] >> 3) & 7)
			if op != at && body[at]&4 != 0 {
				dst += 8
			}
			if dst >= 8 {
				out[n] = 0x45
				n++
			}
			out[n], out[n+1] = 0x89, 0xc0|byte(dst&7)<<3|byte(dst&7)
			n += 2
		}
		movLen := 2 + op - at
		out[n], out[n+1] = 0x70|((body[op+1]&15)^1), byte(movLen)
		n += 2
		n += copy(out[n:], body[at:op])
		out[n], out[n+1] = 0x8b, body[op+2]
		n += 2
		previous = op + 3
	}
	n += copy(out[n:], body[previous:])
	return n, true
}
