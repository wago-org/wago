package gc

import (
	"fmt"
	"os"
)

// WAGO_GC_SUBTYPE_INTERVALS=0 restores validated parent-chain traversal for
// differential qualification without changing collector semantics.
var gcSubtypeIntervalsEnabled = os.Getenv("WAGO_GC_SUBTYPE_INTERVALS") != "0"

// buildSubtypeIntervals assigns one DFS interval to every validated descriptor.
// TypeDesc admits at most one declared supertype, so the descriptor graph is a
// forest and actual is a subtype of required exactly when actual's interval is
// contained by required's interval. The packed table costs eight bytes per type
// and replaces unbounded parent-chain walks on dynamic cast/test hot paths.
func buildSubtypeIntervals(types []TypeDesc) ([]uint64, error) {
	n := len(types)
	if n == 0 {
		return nil, nil
	}
	head := make([]int32, n)
	next := make([]int32, n)
	for i := range n {
		head[i], next[i] = -1, -1
	}
	for i, d := range types {
		if d.ID != TypeID(i) {
			return nil, fmt.Errorf("gc: subtype interval type %d has id %d", i, d.ID)
		}
		if !d.HasSuper {
			continue
		}
		if int(d.Super) >= n {
			return nil, fmt.Errorf("gc: subtype interval type %d has invalid super %d", i, d.Super)
		}
		next[i] = head[d.Super]
		head[d.Super] = int32(i)
	}

	intervals := make([]uint64, n)
	type frame struct {
		node  int32
		child int32
	}
	stack := make([]frame, 0, 32)
	clock := uint32(0)
	for root, d := range types {
		if d.HasSuper {
			continue
		}
		clock++
		intervals[root] = uint64(clock) << 32
		stack = append(stack[:0], frame{node: int32(root), child: head[root]})
		for len(stack) != 0 {
			top := &stack[len(stack)-1]
			if top.child >= 0 {
				child := top.child
				top.child = next[child]
				clock++
				intervals[child] = uint64(clock) << 32
				stack = append(stack, frame{node: child, child: head[child]})
				continue
			}
			clock++
			intervals[top.node] |= uint64(clock)
			stack = stack[:len(stack)-1]
		}
	}
	for i, packed := range intervals {
		if packed == 0 || uint32(packed) == 0 {
			return nil, fmt.Errorf("gc: subtype interval type %d is unreachable", i)
		}
	}
	return intervals, nil
}

func (c *Collector) typeSubtypeIDs(actual, required TypeID) (bool, error) {
	if int(actual) >= len(c.subtypeIntervals) || int(required) >= len(c.subtypeIntervals) {
		return false, fmt.Errorf("gc: unknown subtype pair %d/%d", actual, required)
	}
	dynamic := c.types[actual]
	const shallowParentLimit = 4
	for range shallowParentLimit {
		if dynamic.ID == required {
			return true, nil
		}
		if !dynamic.HasSuper {
			return false, nil
		}
		dynamic = c.types[dynamic.Super]
	}
	if !gcSubtypeIntervalsEnabled {
		for {
			if dynamic.ID == required {
				return true, nil
			}
			if !dynamic.HasSuper {
				return false, nil
			}
			dynamic = c.types[dynamic.Super]
		}
	}
	a, r := c.subtypeIntervals[actual], c.subtypeIntervals[required]
	aPre, aPost := uint32(a>>32), uint32(a)
	rPre, rPost := uint32(r>>32), uint32(r)
	return rPre <= aPre && aPost <= rPost, nil
}
