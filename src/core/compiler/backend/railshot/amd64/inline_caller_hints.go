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

type inlineCallerHints struct {
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
		capacity := min(h.limit, max(1024, 2*cap(h.calls)))
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
	h.ranges[function] = inlineCallerRange{uint32(h.start), end}
}

func (h *inlineCallerHints) plan(function int, targets inlineTargetTable) (inlineCallerPlan, bool) {
	if h == nil || h.disabled || function < 0 || function >= len(h.ranges) {
		return inlineCallerPlan{}, false
	}
	span := h.ranges[function]
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
