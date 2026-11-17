// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import "unsafe"

// inlineCallerHints retains only ordinary call indices from the existing serial
// hint scan. It avoids another bytecode walk when constructing caller plans.
// Unknown entries (AST bodies, parallel scans, or a budget overflow) use the
// original body scanner. The limit bounds optional metadata, never Wasm input.
const maxInlineCallerHintBytes = 4 << 20
const inlineCallerHintHeaderBytes = int(unsafe.Sizeof(inlineCallerHints{}))

const (
	inlineCallerKnown   = uint32(1 << 31)
	inlineCallerBlocked = uint32(1 << 30)
	inlineCallerEndMask = inlineCallerBlocked - 1
)

type inlineCallerRange struct{ start, end uint32 }

type smallInlineCallerRange struct {
	function uint32
	span     inlineCallerRange
}
type smallInlineCallerHints struct{ ranges []smallInlineCallerRange }

const maxSmallInlineCallerHintBytes = 64 << 10

type inlineCallerHints struct {
	small    *smallInlineCallerHints
	calls    []uint32
	ranges   []inlineCallerRange
	limit    int
	start    int
	blocked  bool
	disabled bool
}

func newInlineCallerHints(n int) *inlineCallerHints {
	if n <= 0 || n > (maxInlineCallerHintBytes-inlineCallerHintHeaderBytes)/8 {
		return nil
	}
	return &inlineCallerHints{
		ranges: make([]inlineCallerRange, n),
		limit:  (maxInlineCallerHintBytes - inlineCallerHintHeaderBytes - n*8) / 4,
	}
}

// Small serial modules retain sparse ranges only for actual callers. No-call
// bodies stay unknown; compileFunc never requests their inline plans.
func newSmallInlineCallerHints(n int) *inlineCallerHints {
	if n <= 0 {
		return nil
	}
	return &inlineCallerHints{small: &smallInlineCallerHints{}, limit: maxSmallInlineCallerHintBytes / 4}
}
func (h *inlineCallerHints) drop() {
	h.calls, h.ranges, h.small, h.disabled = nil, nil, nil, true
}
func (h *inlineCallerHints) smallBytes() int {
	return inlineCallerHintHeaderBytes + int(unsafe.Sizeof(smallInlineCallerHints{})) + cap(h.calls)*4 + cap(h.small.ranges)*int(unsafe.Sizeof(smallInlineCallerRange{}))
}

func (h *inlineCallerHints) begin() bool {
	if h == nil || h.disabled {
		return false
	}
	h.start, h.blocked = len(h.calls), false
	return true
}

func (h *inlineCallerHints) record(index uint32) {
	if h.disabled {
		return
	}
	if len(h.calls) == cap(h.calls) {
		if len(h.calls) == h.limit {
			// Drop the complete cache; all callers retain the original scan fallback.
			h.calls, h.ranges, h.disabled = nil, nil, true
			return
		}
		initial := 1024
		if h.small != nil {
			initial = 16
		}
		capacity := min(h.limit, max(initial, 2*cap(h.calls)))
		// Charge old and new backing arrays during a growth copy as well.
		if h.small != nil && h.smallBytes()+capacity*4 > maxSmallInlineCallerHintBytes {
			h.drop()
			return
		}
		calls := make([]uint32, len(h.calls), capacity)
		copy(calls, h.calls)
		h.calls = calls
	}
	h.calls = append(h.calls, index)
}

func (h *inlineCallerHints) finish(function int) {
	if h.disabled {
		return
	}
	end := uint32(len(h.calls)) | inlineCallerKnown
	if h.blocked {
		end |= inlineCallerBlocked
	}
	span := inlineCallerRange{uint32(h.start), end}
	if h.small != nil {
		if len(h.calls) == h.start && !h.blocked {
			return
		}
		rows := h.small.ranges
		if len(rows) == cap(rows) {
			capacity := max(8, 2*cap(rows))
			if h.smallBytes()+capacity*int(unsafe.Sizeof(smallInlineCallerRange{})) > maxSmallInlineCallerHintBytes {
				h.drop()
				return
			}
			next := make([]smallInlineCallerRange, len(rows), capacity)
			copy(next, rows)
			rows = next
		}
		h.small.ranges = append(rows, smallInlineCallerRange{uint32(function), span})
		return
	}
	h.ranges[function] = span
}

func (h *inlineCallerHints) plan(function int, targets inlineTargetTable) (inlineCallerPlan, bool) {
	if h == nil || h.disabled || function < 0 {
		return inlineCallerPlan{}, false
	}
	var span inlineCallerRange
	if h.small != nil {
		rows := h.small.ranges
		lo, hi := 0, len(rows)
		for lo < hi {
			mid := lo + (hi-lo)/2
			if uint64(rows[mid].function) < uint64(function) {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		if lo == len(rows) || uint64(rows[lo].function) != uint64(function) {
			return inlineCallerPlan{}, false
		}
		span = rows[lo].span
	} else {
		if function >= len(h.ranges) {
			return inlineCallerPlan{}, false
		}
		span = h.ranges[function]
	}
	if span.end&inlineCallerKnown == 0 {
		return inlineCallerPlan{}, false
	}
	if targets.empty() {
		return inlineCallerPlan{}, true
	}
	calls := h.calls[span.start : span.end&inlineCallerEndMask]
	plan := inlineCallerPlan{allCallsInline: len(calls) != 0 && span.end&inlineCallerBlocked == 0}
	var accumulate inlineCallerAccumulator
	for _, index := range calls {
		if !accumulate.add(&plan, targets.target(int(index))) {
			plan.allCallsInline = false
		}
	}
	return plan, true
}

// Called only when the existing decoder encounters a call opcode. An entirely
// call-free small module never creates either optional summary object.
func (h *funcHintView) inlineCallerForCall() *inlineCallerHints {
	if h.inlineCaller == nil && h.smallInlineCallerPending {
		h.inlineCaller = newSmallInlineCallerHints(1)
		h.smallInlineCallerPending = false
		h.inlineCaller.begin()
	}
	return h.inlineCaller
}
