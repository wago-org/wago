//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

type managedFunctionEdge struct {
	ref   gc.Ref
	count uint32
}

func (s *managedFunctionCollector) objectShadowEdge(ref gc.Ref, index uint32) managedFunctionEdge {
	if index == 0 {
		return s.shadows[ref]
	}
	return s.extraShadows[ref][index-1]
}
func (s *managedFunctionCollector) setObjectShadowEdge(ref gc.Ref, index uint32, edge managedFunctionEdge) {
	if index == 0 {
		s.shadows[ref] = edge
		return
	}
	if s.extraShadows == nil {
		s.extraShadows = make(map[gc.Ref][]managedFunctionEdge)
	}
	extra := s.extraShadows[ref]
	if int(index-1) == len(extra) {
		extra = append(extra, edge)
	} else {
		extra[index-1] = edge
	}
	s.extraShadows[ref] = extra
}

// Scratch is cold and reusable. It roots every prepared producer until the
// destination object has a traced edge, including allocations made by a later
// initializer. The raw function words always remain native descriptors.
type managedFunctionAggregateScratch struct {
	parent gc.RootSet
	roots  []gc.Root
	edges  []managedFunctionEdge
}

func (s *managedFunctionAggregateScratch) RangeRoots(fn func(gc.RootSlot) bool) {
	if s.parent != nil {
		more := true
		s.parent.RangeRoots(func(slot gc.RootSlot) bool { more = fn(slot); return more })
		if !more {
			return
		}
	}
	for i := range s.roots {
		if !fn(&s.roots[i]) {
			return
		}
	}
}
func (s *managedFunctionAggregateScratch) RangeRootRefs(sink gc.RootRefSink) bool {
	if s.parent != nil {
		if direct, ok := s.parent.(gc.DirectRootRefSet); ok {
			if !direct.RangeRootRefs(sink) {
				return false
			}
		} else {
			more := true
			s.parent.RangeRoots(func(slot gc.RootSlot) bool { more = sink.VisitRootRef(slot.GetRef()); return more })
			if !more {
				return false
			}
		}
	}
	for i := range s.roots {
		if !sink.VisitRootRef(s.roots[i].GetRef()) {
			return false
		}
	}
	return true
}
func (s *managedFunctionAggregateScratch) clear() {
	s.parent = nil
	clear(s.roots)
	s.roots = s.roots[:0]
	clear(s.edges)
	s.edges = s.edges[:0]
}
func (s *managedFunctionCollector) aggregateScratch(parent gc.RootSet, object gc.Ref) *managedFunctionAggregateScratch {
	if s.aggregate == nil {
		s.aggregate = &managedFunctionAggregateScratch{}
	}
	a := s.aggregate
	if a.parent != nil || len(a.roots) != 0 || len(a.edges) != 0 {
		panic(gcHelperFailuref("reentrant function aggregate scratch"))
	}
	a.parent = parent
	for i := uint32(0); i < s.ObjectEdgeCount(object); i++ {
		a.edges = append(a.edges, s.objectShadowEdge(object, i))
	}
	for _, edge := range a.edges {
		if !edge.ref.IsNull() {
			a.roots = append(a.roots, gc.Root(edge.ref))
		}
	}
	return a
}
func functionStorage(kind gc.StorageKind) bool {
	return kind == gc.StorageFuncRef || kind == gc.StorageFuncRefNull
}
func (a *managedFunctionAggregateScratch) change(s *managedFunctionCollector, in *Instance, bits uint64, delta int64, allocate bool) {
	if bits == 0 || delta == 0 {
		return
	}
	var ref gc.Ref
	if allocate {
		var err error
		ref, err = s.environmentForDescriptor(in, bits, a)
		if err != nil {
			panic(gcHelperFailure(err))
		}
	} else {
		env := s.byDescriptor[bits]
		if env == nil {
			panic(gcHelperFailuref("function aggregate descriptor has no managed producer edge"))
		}
		ref = env.root.GetRef()
	}
	a.changeRef(ref, delta)
}
func (a *managedFunctionAggregateScratch) changeRef(ref gc.Ref, delta int64) {
	for i := range a.edges {
		if a.edges[i].ref != ref {
			continue
		}
		count := int64(a.edges[i].count) + delta
		if count < 0 || count > int64(^uint32(0)) {
			panic(gcHelperFailuref("invalid function aggregate edge count"))
		}
		a.edges[i].count = uint32(count)
		// Keep the old edge rooted until commit, including when its count is zero.
		return
	}
	if delta < 0 || delta > int64(^uint32(0)) {
		panic(gcHelperFailuref("missing function aggregate old edge"))
	}
	a.edges = append(a.edges, managedFunctionEdge{ref: ref, count: uint32(delta)})
	a.roots = append(a.roots, gc.Root(ref))
}
func (a *managedFunctionAggregateScratch) publish(s *managedFunctionCollector, object gc.Ref) {
	// Stable indexes matter while Tiny has a saved object scan cursor. Removed
	// producers become null tombstones; later producers reuse an empty slot.
	for _, edge := range a.edges {
		found, empty := -1, -1
		count := s.ObjectEdgeCount(object)
		for i := uint32(0); i < count; i++ {
			old := s.objectShadowEdge(object, i)
			if old.ref == edge.ref {
				found = int(i)
				break
			}
			if old.ref.IsNull() {
				empty = int(i)
			}
		}
		if found >= 0 {
			if edge.count == 0 {
				edge.ref = gc.Null()
			}
			s.setObjectShadowEdge(object, uint32(found), edge)
		} else if edge.count != 0 {
			index := count
			if empty >= 0 {
				index = uint32(empty)
			}
			s.setObjectShadowEdge(object, index, edge)
		}
		if edge.count != 0 {
			s.collector.WriteBarrierObject(object, edge.ref)
		}
	}
}
