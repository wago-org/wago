//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import gc "github.com/wago-org/wago/src/core/runtime/gc/native"

// This scratch exists only after a non-null opaque-reference capture. Ordinary
// numeric exceptions keep their160-byte object and16-byte weak record layout.
type managedFunctionExceptionScratch struct {
	words  [34]uint64
	envs   [8]gc.Root
	count  uint8
	parent gc.RootSet
	types  [9]gc.TypeID
	made   uint16
}

func (s *managedFunctionExceptionScratch) RangeRoots(fn func(gc.RootSlot) bool) {
	if s.parent != nil {
		keepGoing := true
		s.parent.RangeRoots(func(slot gc.RootSlot) bool { keepGoing = fn(slot); return keepGoing })
		if !keepGoing {
			return
		}
	}
	for i := uint8(0); i < s.count; i++ {
		if !fn(&s.envs[i]) {
			return
		}
	}
}
func (s *managedFunctionExceptionScratch) RangeRootRefs(sink gc.RootRefSink) bool {
	if s.parent != nil {
		if direct, ok := s.parent.(gc.DirectRootRefSet); ok {
			if !direct.RangeRootRefs(sink) {
				return false
			}
		} else {
			keepGoing := true
			s.parent.RangeRoots(func(slot gc.RootSlot) bool { keepGoing = sink.VisitRootRef(slot.GetRef()); return keepGoing })
			if !keepGoing {
				return false
			}
		}
	}
	for i := uint8(0); i < s.count; i++ {
		if !sink.VisitRootRef(s.envs[i].GetRef()) {
			return false
		}
	}
	return true
}
func (s *managedFunctionExceptionScratch) clear() {
	clear(s.words[:])
	clear(s.envs[:])
	s.parent = nil
	s.count = 0
}

func (s *managedFunctionCollector) exceptionScratch(parent gc.RootSet) *managedFunctionExceptionScratch {
	if s.exception == nil {
		s.exception = &managedFunctionExceptionScratch{}
	}
	scratch := s.exception
	if scratch.parent != nil || scratch.count != 0 {
		panic(gcHelperFailuref("reentrant managed function exception scratch"))
	}
	scratch.parent = parent
	return scratch
}

func ownedExceptionFieldKinds(extra int) []gc.StorageKind {
	fields := make([]gc.StorageKind, 0, 26+extra)
	for i := 0; i < 9; i++ {
		fields = append(fields, gc.StorageI64)
	}
	for i := 0; i < 8; i++ {
		fields = append(fields, gc.StorageRefNull, gc.StorageI32)
	}
	fields = append(fields, gc.StorageI64)
	for i := 0; i < extra; i++ {
		fields = append(fields, gc.StorageRefNull)
	}
	return fields
}

func (in *Instance) managedExceptionType(s *managedFunctionCollector, domain *gcStoreDomain, count uint8) gc.TypeID {
	if count == 0 || count > 8 {
		panic(gcHelperFailuref("invalid opaque exception field count"))
	}
	scratch := s.exception
	if scratch.made&(1<<count) != 0 {
		return scratch.types[count]
	}
	id := gc.TypeID(in.gc.TypeCount())
	desc, err := gc.NewStructDesc(id, ownedExceptionFieldKinds(int(count)))
	if err != nil {
		panic(gcHelperFailure(err))
	}
	if err = in.gc.AddTypes([]gc.TypeDesc{desc}); err != nil {
		panic(gcHelperFailure(err))
	}
	if domain != nil {
		in.refStore.mu.Lock()
		domain.types = append(domain.types, desc)
		domain.typeReps = append(domain.typeReps, gcDomainTypeRepresentative{})
		in.refStore.mu.Unlock()
	}
	scratch.types[count] = id
	scratch.made |= 1 << count
	return id
}

func managedExceptionPayloadType(collector *gc.Collector, actual gc.TypeID) bool {
	state, ok := managedFunctionCollectors.Load(collector)
	if !ok {
		return false
	}
	scratch := state.(*managedFunctionCollector).exception
	if scratch == nil {
		return false
	}
	for count := uint8(1); count <= 8; count++ {
		if scratch.made&(1<<count) != 0 && scratch.types[count] == actual {
			return true
		}
	}
	return false
}
