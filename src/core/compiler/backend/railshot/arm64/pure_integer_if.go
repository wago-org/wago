// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import "github.com/wago-org/wago/src/core/compiler/wasm"

var pureIntegerIfSelectEnabled = true

type pureIntegerIfPlan struct {
	arms  [2]wasm.Reader
	stops [2]int
	end   int
}

// Only bounded, nontrapping integer expressions may be evaluated on both paths.
// Each arm must build its own result without consuming the caller's operands.
func (f *fn) scanPureIntegerIf(r wasm.Reader, typ machineType) (pureIntegerIfPlan, bool) {
	var plan pureIntegerIfPlan
	if typ != mtI32 && typ != mtI64 {
		return plan, false
	}
	start, totalCost := r.Offset(), 0
	for arm := range plan.arms {
		plan.arms[arm] = r
		depth, cost := 0, 0
		for count := 0; ; count++ {
			if count > 8 || r.Offset()-start > 64 {
				return plan, false
			}
			pos := r.Offset()
			op, err := r.Byte()
			if err != nil {
				return plan, false
			}
			if op == 0x05 || op == 0x0b {
				want := byte(0x05)
				if arm == 1 {
					want = 0x0b
				}
				if op != want || depth != 1 {
					return plan, false
				}
				plan.stops[arm] = pos
				break
			}
			switch {
			case op == 0x01:
			case op == 0x20:
				x, err := r.U32()
				absolute := uint64(x) + uint64(f.localBase)
				if err != nil || absolute >= uint64(len(f.localType)) || f.localType[absolute] != typ {
					return plan, false
				}
				depth++
			case op == 0x41 && typ == mtI32:
				if _, err := r.I32(); err != nil {
					return plan, false
				}
				depth++
			case op == 0x42 && typ == mtI64:
				if _, err := r.I64(); err != nil {
					return plan, false
				}
				depth++
			default:
				base := byte(0x6a)
				if typ == mtI64 {
					base = 0x7c
				}
				delta := int(op) - int(base)
				if depth < 2 || !(delta >= 0 && delta <= 2 || delta >= 7 && delta <= 14) {
					return plan, false
				}
				depth--
				cost++
				if delta == 2 {
					cost++
				}
			}
			if depth > 4 || cost > 4 {
				return plan, false
			}
		}
		totalCost += cost
	}
	if r.Offset()-start > 64 || totalCost > 6 {
		return plan, false
	}
	plan.end = r.Offset()
	return plan, true
}

func (f *fn) tryPureIntegerIfSelect(r *wasm.Reader, typ machineType) (bool, error) {
	if !f.opt(optPureIntegerIfSelect) || profileEnabled || f.moduleEH || f.gcFrameRoots != nil || f.policy.CompactNative ||
		len(f.customInstructions) != 0 || len(f.intervalLast) != 0 || f.intervalControl || f.selectGroupEnd != 0 || f.streamingReduction || f.localBase != 0 {
		return false, nil
	}
	plan, ok := f.scanPureIntegerIf(*r, typ)
	if !ok {
		return false, nil
	}
	if f.s.head.prev == sentinelNodeID {
		return false, nil
	}
	if err := r.JumpTo(plan.end); err != nil {
		return false, err
	}
	condition := f.s.head.prev
	for arm, reader := range plan.arms {
		for reader.Offset() < plan.stops[arm] {
			op, err := reader.Byte()
			if err != nil {
				return true, err
			}
			if op == 0x01 {
				continue
			}
			if err := f.emitPlain(&reader, op); err != nil {
				return true, err
			}
		}
	}
	// Both arms are pure and read unchanged locals. Move the condition's entire
	// current tree after them, then reuse select's ownership and flags handling.
	f.s.moveValentBlockToTop(condition)
	f.emitSelect()
	f.stats.peep("pure-integer-if-select")
	return true, nil
}

// Relinking retains arena IDs, deferred-node counts and all child relationships.
// Allocation while building the arms may already have condensed this root.
func (s *stack) moveValentBlockToTop(rootID nodeID) {
	if rootID == s.head.prev {
		return
	}
	root := s.node(rootID)
	first := s.baseOfValentBlock(root)
	firstID, before, after, last := s.prev(first).next, first.prev, root.next, s.head.prev
	s.node(before).next, s.node(after).prev = after, before
	first.prev, s.node(last).next = last, firstID
	root.next, s.head.prev = sentinelNodeID, rootID
}
