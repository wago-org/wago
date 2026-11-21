// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "unsafe"

// inlineCallerHints retains calls in source order from the serial hint scan.
// Only bodies that saw calls get range records. If the optional budget is
// exhausted, the original caller scan remains the fallback for every function.
const (
	maxInlineCallerHintBytes   = 4 << 20
	smallInlineCallerHintBytes = 32 << 10
	minInlineCallerHintBody    = 128
	largeInlineCallerModule    = 64 << 10
	inlineCallerBlocked        = uint32(1 << 31)
	inlineCallerEndMask        = inlineCallerBlocked - 1
)

type inlineCallerRange struct{ function, end uint32 }

type inlineCallerHints struct {
	calls    []uint32
	ranges   []inlineCallerRange // the previous end is the next range's start
	budget   int
	blocked  bool
	disabled bool
}

const (
	inlineCallerHintHeaderBytes = int(unsafe.Sizeof(inlineCallerHints{}))
	inlineCallerRangeBytes      = int(unsafe.Sizeof(inlineCallerRange{}))
	inlineCallerWordBytes       = int(unsafe.Sizeof(uint32(0)))
)

func newInlineCallerHints(n, budget int) *inlineCallerHints {
	if n <= 0 || uint64(n) > uint64(^uint32(0)) || budget < inlineCallerHintHeaderBytes {
		return nil
	}
	return &inlineCallerHints{budget: min(budget, maxInlineCallerHintBytes)}
}

func (h *inlineCallerHints) storageBytes() int {
	return inlineCallerHintHeaderBytes + cap(h.calls)*inlineCallerWordBytes + cap(h.ranges)*inlineCallerRangeBytes
}

func (h *inlineCallerHints) begin() bool {
	if h == nil || h.disabled {
		return false
	}
	h.blocked = false
	return true
}

func (h *inlineCallerHints) disable() {
	h.calls, h.ranges, h.disabled = nil, nil, true
}

func (h *inlineCallerHints) record(index uint32) {
	if h.disabled {
		return
	}
	if len(h.calls) == cap(h.calls) {
		limit := (h.budget - inlineCallerHintHeaderBytes - cap(h.ranges)*inlineCallerRangeBytes) / inlineCallerWordBytes
		if len(h.calls) >= limit {
			h.disable()
			return
		}
		calls := make([]uint32, len(h.calls), min(limit, max(8, 2*cap(h.calls))))
		copy(calls, h.calls)
		h.calls = calls
	}
	h.calls = append(h.calls, index)
}

func (h *inlineCallerHints) finish(function int) {
	if h.disabled {
		return
	}
	var start uint32
	if n := len(h.ranges); n != 0 {
		start = h.ranges[n-1].end & inlineCallerEndMask
	}
	end := uint32(len(h.calls))
	if start == end && !h.blocked {
		return
	}
	if len(h.ranges) == cap(h.ranges) {
		limit := (h.budget - inlineCallerHintHeaderBytes - cap(h.calls)*inlineCallerWordBytes) / inlineCallerRangeBytes
		if len(h.ranges) >= limit {
			h.disable()
			return
		}
		ranges := make([]inlineCallerRange, len(h.ranges), min(limit, max(4, 2*cap(h.ranges))))
		copy(ranges, h.ranges)
		h.ranges = ranges
	}
	if h.blocked {
		end |= inlineCallerBlocked
	}
	h.ranges = append(h.ranges, inlineCallerRange{uint32(function), end})
}

func (h *inlineCallerHints) plan(function int, targets inlineTargetTable) (inlineCallerPlan, bool) {
	if h == nil || h.disabled || function < 0 {
		return inlineCallerPlan{}, false
	}
	lo, hi := 0, len(h.ranges)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if int(h.ranges[mid].function) < function {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == len(h.ranges) || int(h.ranges[lo].function) != function {
		return inlineCallerPlan{}, false
	}
	span := h.ranges[lo]
	if targets.empty() {
		return inlineCallerPlan{}, true
	}
	var start uint32
	if lo != 0 {
		start = h.ranges[lo-1].end & inlineCallerEndMask
	}
	calls := h.calls[start : span.end&inlineCallerEndMask]
	plan := inlineCallerPlan{allCallsInline: len(calls) != 0 && span.end&inlineCallerBlocked == 0}
	var accumulate inlineCallerAccumulator
	for _, index := range calls {
		if !accumulate.add(&plan, targets.target(int(index))) {
			plan.allCallsInline = false
		}
	}
	return plan, true
}
