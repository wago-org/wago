package shared

import "slices"

// GlobalHint is the compact per-function record retained for a referenced
// global. Modules with sparse global use keep one record per actual use target
// instead of a functions-by-globals matrix.
type GlobalHint struct {
	Index    uint32
	Score    uint32
	Eligible bool
}

// GlobalHintAccumulator provides dense O(globals) scratch while retaining only
// sparse records. One accumulator is reset and reused for every serially scanned
// function; epoch marks avoid clearing the dense scratch between functions.
type GlobalHintAccumulator struct {
	scores        []uint32
	marks         []uint32
	epoch         uint32
	touchedInline [32]uint32
	touchedN      uint8
	touchedExtra  []uint32
}

const (
	globalHintEligible  = uint32(1 << 31)
	globalHintEpochMask = globalHintEligible - 1
)

func (a *GlobalHintAccumulator) Reset(nGlobals int) {
	if len(a.scores) < nGlobals {
		words := make([]uint32, 2*nGlobals)
		a.scores = words[:nGlobals:nGlobals]
		a.marks = words[nGlobals:]
	}
	a.epoch = (a.epoch + 1) & globalHintEpochMask
	if a.epoch == 0 {
		clear(a.marks)
		a.epoch = 1
	}
	a.touchedN = 0
	a.touchedExtra = a.touchedExtra[:0]
}

func (a *GlobalHintAccumulator) touch(index uint32) bool {
	if int(index) >= len(a.scores) {
		return false
	}
	if a.marks[index]&globalHintEpochMask != a.epoch {
		a.marks[index] = a.epoch
		a.scores[index] = 0
		if int(a.touchedN) < len(a.touchedInline) {
			a.touchedInline[a.touchedN] = index
			a.touchedN++
		} else {
			a.touchedExtra = append(a.touchedExtra, index)
		}
	}
	return true
}

func (a *GlobalHintAccumulator) Add(index uint32, delta int64) {
	if delta <= 0 || !a.touch(index) {
		return
	}
	const max = ^uint32(0)
	if uint64(a.scores[index])+uint64(delta) >= uint64(max) {
		a.scores[index] = max
	} else {
		a.scores[index] += uint32(delta)
	}
}

func (a *GlobalHintAccumulator) MarkEligible(index uint32) {
	if a.touch(index) {
		a.marks[index] |= globalHintEligible
	}
}

// AppendTo appends deterministic index-sorted records to dst. Callers can keep
// offset ranges while dst grows, then publish slices after the final append.
func (a *GlobalHintAccumulator) AppendTo(dst []GlobalHint) []GlobalHint {
	inline := a.touchedInline[:a.touchedN]
	slices.Sort(inline)
	slices.Sort(a.touchedExtra)
	appendIndex := func(index uint32) {
		dst = append(dst, GlobalHint{Index: index, Score: a.scores[index], Eligible: a.marks[index]&globalHintEligible != 0})
	}
	i, j := 0, 0
	for i < len(inline) && j < len(a.touchedExtra) {
		if inline[i] < a.touchedExtra[j] {
			appendIndex(inline[i])
			i++
		} else {
			appendIndex(a.touchedExtra[j])
			j++
		}
	}
	for ; i < len(inline); i++ {
		appendIndex(inline[i])
	}
	for ; j < len(a.touchedExtra); j++ {
		appendIndex(a.touchedExtra[j])
	}
	return dst
}
