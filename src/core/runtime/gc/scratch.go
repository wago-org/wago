package gc

import raw "github.com/wago-org/wago/src/core/runtime/gc/native"

// Each collector retains at most one idle scratch record and at most 256
// entries per vector. Reentrant callbacks borrow a separate record; outliers
// are discarded on return. No scratch state is shared across collectors.
const maxScratchEntries = 256

type checkedScratch struct {
	refs    []Ref
	native  raw.RefSliceRoots
	inputs  []raw.Value
	visit   func(RootSlot) bool
	failure error
}

func (s *checkedScratch) visitRoot(slot RootSlot) bool {
	if s.failure != nil {
		return false
	}
	if slot == nil {
		s.failure = ErrInvalidReference
		return false
	}
	if len(s.refs) == cap(s.refs) && len(s.refs) < maxScratchEntries {
		capacity := min(maxScratchEntries, max(8, cap(s.refs)*2))
		refs := make([]Ref, len(s.refs), capacity)
		copy(refs, s.refs)
		s.refs = refs
	}
	s.refs = append(s.refs, slot.GetRef())
	return true
}
func (s *checkedScratch) RangeRoots(visit func(raw.RootSlot) bool) { s.native.RangeRoots(visit) }
func (s *checkedScratch) RangeRootRefs(sink raw.RootRefSink) bool {
	return s.native.RangeRootRefs(sink)
}
func (s *checkedScratch) inputValues() []raw.Value {
	if s == nil {
		return nil
	}
	return s.inputs
}

func (c *Collector) releaseScratch(s *checkedScratch) {
	if s == nil {
		return
	}
	clear(s.refs)
	clear(s.native)
	clear(s.inputs)
	s.refs, s.native, s.inputs = s.refs[:0], s.native[:0], s.inputs[:0]
	if cap(s.refs) > maxScratchEntries {
		s.refs = nil
	}
	if cap(s.native) > maxScratchEntries {
		s.native = nil
	}
	if cap(s.inputs) > maxScratchEntries {
		s.inputs = nil
	}
	s.failure = nil
	if !c.closed && c.scratch == nil {
		c.scratch = s
	}
}

// Caller defers releaseScratch after success and keeps the record until native
// consumption ends. All user callbacks finish before owner/generation checks.
// The error and panic paths return the record here, before ownership transfers.
func (c *Collector) prepareScratch(roots RootSet, values []Value) (scratch *checkedScratch, nativeRoots raw.RootSet, err error) {
	if err := c.available(); err != nil {
		return nil, nil, err
	}
	if roots == nil && len(values) == 0 {
		return nil, nil, nil
	}
	s := c.scratch
	c.scratch = nil
	if s == nil {
		s = &checkedScratch{}
		s.visit = s.visitRoot
	}
	transferred := false
	defer func() {
		if !transferred {
			c.releaseScratch(s)
		}
	}()
	if roots != nil {
		roots.RangeRoots(s.visit)
	}
	if err := c.available(); err != nil {
		return nil, nil, err
	}
	if s.failure != nil {
		return nil, nil, s.failure
	}
	if cap(s.native) < len(s.refs) {
		s.native = make(raw.RefSliceRoots, len(s.refs))
	} else {
		s.native = s.native[:len(s.refs)]
	}
	for i, ref := range s.refs {
		value, err := c.unwrap(ref)
		if err != nil {
			return nil, nil, err
		}
		s.native[i] = value
	}
	if cap(s.inputs) < len(values) {
		s.inputs = make([]raw.Value, len(values))
	} else {
		s.inputs = s.inputs[:len(values)]
	}
	for i, value := range values {
		input, err := c.input(value)
		if err != nil {
			return nil, nil, err
		}
		s.inputs[i] = input
	}
	if roots != nil {
		nativeRoots = s
	}
	transferred = true
	return s, nativeRoots, nil
}
