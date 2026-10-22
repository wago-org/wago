//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"sync"
	"sync/atomic"
)

// Construction has no Instance descriptor directory until registration. Its
// journal keeps only new opaque-reference objects alive, while the builder's
// frozen attachments retain every producer. Binding happens before start and
// before the shared collector's construction lease is released.
var managedFunctionConstructions sync.Map // *gc.Collector -> *managedFunctionConstruction
var managedFunctionConstructionCount atomic.Uint32

type managedFunctionConstructedObject struct {
	ref   gc.Root
	local uint32
}
type managedFunctionConstruction struct {
	c         *Compiled
	collector *gc.Collector
	parent    *gc.Slots
	objects   []managedFunctionConstructedObject
}

func newManagedFunctionConstruction(c *Compiled, collector *gc.Collector, parent *gc.Slots) *managedFunctionConstruction {
	if c == nil || collector == nil || !c.usesManagedOpaqueStorage() {
		return nil
	}
	has := false
	for _, desc := range c.GCTypeDescs {
		if desc.Kind == gc.KindArray {
			has = has || opaqueAggregateStorage(desc.Elem)
		} else if desc.Kind == gc.KindStruct {
			for _, field := range desc.Fields {
				has = has || opaqueAggregateStorage(field.Kind)
			}
		}
	}
	if !has {
		return nil
	}
	journal := &managedFunctionConstruction{c: c, collector: collector, parent: parent}
	if _, loaded := managedFunctionConstructions.LoadOrStore(collector, journal); loaded {
		panic(gcHelperFailuref("overlapping function construction in one collector"))
	}
	managedFunctionConstructionCount.Add(1)
	return journal
}
func (j *managedFunctionConstruction) roots(fallback gc.Slots) gc.RootSet {
	if j == nil {
		return fallback
	}
	return j
}
func (j *managedFunctionConstruction) RangeRoots(fn func(gc.RootSlot) bool) {
	more := true
	if j.parent != nil {
		(*j.parent).RangeRoots(func(slot gc.RootSlot) bool { more = fn(slot); return more })
	}
	if !more {
		return
	}
	for i := range j.objects {
		if !fn(&j.objects[i].ref) {
			return
		}
	}
}
func (j *managedFunctionConstruction) RangeRootRefs(sink gc.RootRefSink) bool {
	if j.parent != nil {
		more := true
		(*j.parent).RangeRoots(func(slot gc.RootSlot) bool { more = sink.VisitRootRef(slot.GetRef()); return more })
		if !more {
			return false
		}
	}
	for i := range j.objects {
		if !sink.VisitRootRef(j.objects[i].ref.GetRef()) {
			return false
		}
	}
	return true
}
func (j *managedFunctionConstruction) close() {
	if j == nil {
		return
	}
	if managedFunctionConstructions.CompareAndDelete(j.collector, j) {
		managedFunctionConstructionCount.Add(^uint32(0))
	}
	clear(j.objects)
	j.objects = nil
	j.parent = nil
}
func recordManagedFunctionConstruction(collector *gc.Collector, c *Compiled, ref gc.Ref, local uint32) {
	if managedFunctionConstructionCount.Load() == 0 || ref.IsNull() {
		return
	}
	value, ok := managedFunctionConstructions.Load(collector)
	if !ok {
		return
	}
	j := value.(*managedFunctionConstruction)
	if j.c != c || int(local) >= len(c.GCTypeDescs) {
		return
	}
	desc := &c.GCTypeDescs[local]
	has := desc.Kind == gc.KindArray && opaqueAggregateStorage(desc.Elem)
	if desc.Kind == gc.KindStruct {
		for _, field := range desc.Fields {
			has = has || opaqueAggregateStorage(field.Kind)
		}
	}
	if has {
		j.objects = append(j.objects, managedFunctionConstructedObject{ref: gc.Root(ref), local: local})
	}
}
func (j *managedFunctionConstruction) bind(in *Instance, domain *gcStoreDomain) error {
	if j == nil || len(j.objects) == 0 {
		return nil
	}
	if in.refStore == nil && in.c.compactExternFrameRoots() {
		if _, err := in.compactExternCollector(domain); err != nil {
			return err
		}
	}
	state := in.managedFunctionCollector(domain)
	for _, object := range j.objects {
		ref := object.ref.GetRef()
		desc := &j.c.GCTypeDescs[object.local]
		a := state.aggregateScratch(j, ref)
		func() {
			defer a.clear()
			switch desc.Kind {
			case gc.KindStruct:
				for i, field := range desc.Fields {
					if !opaqueAggregateStorage(field.Kind) {
						continue
					}
					value, err := in.gc.StructGet(ref, uint32(i))
					if err != nil {
						panic(gcHelperFailure(err))
					}
					normalized := in.managedOpaqueTypedValue(field.Kind, in.c.Types[object.local].Fields[i].Storage.Value, value.Bits)
					if normalized.Bits != value.Bits {
						if err := in.gc.StructSet(ref, uint32(i), normalized); err != nil {
							panic(gcHelperFailure(err))
						}
					}
					a.changeOpaque(state, in, field.Kind, normalized.Bits, 1, true)
				}
			case gc.KindArray:
				length, err := in.gc.ArrayLen(ref)
				if err != nil {
					panic(gcHelperFailure(err))
				}
				for i := uint32(0); i < length; i++ {
					value, err := in.gc.ArrayGet(ref, i)
					if err != nil {
						panic(gcHelperFailure(err))
					}
					normalized := in.managedFunctionArrayValue(object.local, value.Bits)
					if normalized.Bits != value.Bits {
						if err := in.gc.ArraySet(ref, i, normalized); err != nil {
							panic(gcHelperFailure(err))
						}
					}
					a.changeOpaque(state, in, desc.Elem, normalized.Bits, 1, true)
				}
			default:
				panic(gcHelperFailure(fmt.Errorf("invalid function construction object kind")))
			}
			a.publish(state, ref)
		}()
	}
	return nil
}
