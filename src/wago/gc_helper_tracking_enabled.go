//go:build wago_gcstats

package wago

import (
	"sync/atomic"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

type gcHelperCounter struct {
	collector                         *gc.Collector
	calls                             atomic.Uint64
	allocCalls                        atomic.Uint64
	structAllocCalls                  atomic.Uint64
	arrayAllocCalls                   atomic.Uint64
	structDefaultAllocCalls           atomic.Uint64
	structInitializedAllocCalls       atomic.Uint64
	arrayDefaultAllocCalls            atomic.Uint64
	arrayOtherAllocCalls              atomic.Uint64
	mutations                         atomic.Uint64
	structMutations                   atomic.Uint64
	arrayMutations                    atomic.Uint64
	referenceMutations                atomic.Uint64
	parentNurseryMutations            atomic.Uint64
	parentOldMutations                atomic.Uint64
	parentLargeMutations              atomic.Uint64
	parentTinyMutations               atomic.Uint64
	oldYoungRememberedMutations       atomic.Uint64
	oldYoungUnrememberedMutations     atomic.Uint64
	structOldYoungRememberedMutations atomic.Uint64
	arrayOldYoungRememberedMutations  atomic.Uint64
	arrayCardPresentMutations         atomic.Uint64
	arrayCardCoveredMutations         atomic.Uint64
}

var activeGCHelperCounter atomic.Pointer[gcHelperCounter]

func recordSynchronousGCHelper(in *Instance, helper uint32, args []uint64) {
	if in == nil {
		return
	}
	counter := activeGCHelperCounter.Load()
	if counter == nil || counter.collector != in.gc {
		return
	}
	counter.calls.Add(1)
	if gcHelperMayAllocate(helper) {
		counter.allocCalls.Add(1)
		if helper < gcArrayAllocDefault {
			counter.structAllocCalls.Add(1)
			if helper == gcStructAllocDefault {
				counter.structDefaultAllocCalls.Add(1)
			} else {
				counter.structInitializedAllocCalls.Add(1)
			}
		} else {
			counter.arrayAllocCalls.Add(1)
			if helper == gcArrayAllocDefault || helper == gcArrayAllocDefaultNative {
				counter.arrayDefaultAllocCalls.Add(1)
			} else {
				counter.arrayOtherAllocCalls.Add(1)
			}
		}
	}
	if gcHelperMayMutate(helper) {
		counter.mutations.Add(1)
		if helper < gcArrayAllocDefault {
			counter.structMutations.Add(1)
		} else {
			counter.arrayMutations.Add(1)
		}
		if parent, child, ok := diagnosticReferenceMutation(in, helper, args); ok {
			counter.referenceMutations.Add(1)
			parentSpace, childSpace, remembered := in.gc.DiagnosticObjectStore(parent, child)
			switch parentSpace {
			case gc.DiagnosticSpaceNursery:
				counter.parentNurseryMutations.Add(1)
			case gc.DiagnosticSpaceOld:
				counter.parentOldMutations.Add(1)
				if childSpace == gc.DiagnosticSpaceNursery {
					if remembered {
						counter.oldYoungRememberedMutations.Add(1)
						if helper == gcStructSet {
							counter.structOldYoungRememberedMutations.Add(1)
						} else if helper == gcArraySet {
							counter.arrayOldYoungRememberedMutations.Add(1)
						}
					} else {
						counter.oldYoungUnrememberedMutations.Add(1)
					}
				}
			case gc.DiagnosticSpaceLarge:
				counter.parentLargeMutations.Add(1)
			case gc.DiagnosticSpaceTiny:
				counter.parentTinyMutations.Add(1)
			}
			if helper == gcArraySet && len(args) >= 2 {
				present, covered := in.gc.DiagnosticArrayCard(parent, uint32(args[1]))
				if present {
					counter.arrayCardPresentMutations.Add(1)
				}
				if covered {
					counter.arrayCardCoveredMutations.Add(1)
				}
			}
		}
	}
}

func diagnosticReferenceMutation(in *Instance, helper uint32, args []uint64) (parent, child gc.Ref, ok bool) {
	if in == nil || in.c == nil {
		return 0, 0, false
	}
	switch helper {
	case gcStructSet:
		if len(args) < 4 {
			return 0, 0, false
		}
		typeID, fieldID := uint32(args[len(args)-2]), uint32(args[len(args)-1])
		if int(typeID) >= len(in.c.GCTypeDescs) || int(fieldID) >= len(in.c.GCTypeDescs[typeID].Fields) {
			return 0, 0, false
		}
		desc := in.c.GCTypeDescs[typeID]
		if !desc.Final || (desc.Fields[fieldID].Kind != gc.StorageRef && desc.Fields[fieldID].Kind != gc.StorageRefNull) {
			return 0, 0, false
		}
		return gc.Ref(uint32(args[0])), gc.Ref(uint32(args[1])), true
	case gcArraySet:
		if len(args) < 4 {
			return 0, 0, false
		}
		typeID := uint32(args[len(args)-1])
		if int(typeID) >= len(in.c.GCTypeDescs) {
			return 0, 0, false
		}
		desc := in.c.GCTypeDescs[typeID]
		if !desc.Final || (desc.Elem != gc.StorageRef && desc.Elem != gc.StorageRefNull) {
			return 0, 0, false
		}
		return gc.Ref(uint32(args[0])), gc.Ref(uint32(args[2])), true
	default:
		return 0, 0, false
	}
}

func setGCHelperStatsTracking(collector *gc.Collector, enabled bool) {
	if !enabled {
		counter := activeGCHelperCounter.Load()
		if counter != nil && counter.collector == collector {
			activeGCHelperCounter.CompareAndSwap(counter, nil)
		}
		return
	}
	activeGCHelperCounter.Store(&gcHelperCounter{collector: collector})
}

func snapshotGCHelperStats(collector *gc.Collector) GCHelperStats {
	counter := activeGCHelperCounter.Load()
	if counter == nil || counter.collector != collector {
		return GCHelperStats{}
	}
	return GCHelperStats{
		Calls:                            counter.calls.Load(),
		AllocationCalls:                  counter.allocCalls.Load(),
		StructAllocationCalls:            counter.structAllocCalls.Load(),
		ArrayAllocationCalls:             counter.arrayAllocCalls.Load(),
		StructDefaultAllocationCalls:     counter.structDefaultAllocCalls.Load(),
		StructInitializedAllocationCalls: counter.structInitializedAllocCalls.Load(),
		ArrayDefaultAllocationCalls:      counter.arrayDefaultAllocCalls.Load(),
		ArrayOtherAllocationCalls:        counter.arrayOtherAllocCalls.Load(),
		MutationCalls:                    counter.mutations.Load(),
		StructMutationCalls:              counter.structMutations.Load(),
		ArrayMutationCalls:               counter.arrayMutations.Load(),
		ReferenceMutationCalls:           counter.referenceMutations.Load(),
		ParentNurseryMutationCalls:       counter.parentNurseryMutations.Load(),
		ParentOldMutationCalls:           counter.parentOldMutations.Load(),
		ParentLargeMutationCalls:         counter.parentLargeMutations.Load(),
		ParentTinyMutationCalls:          counter.parentTinyMutations.Load(),
		OldYoungRememberedCalls:          counter.oldYoungRememberedMutations.Load(),
		OldYoungUnrememberedCalls:        counter.oldYoungUnrememberedMutations.Load(),
		StructOldYoungRememberedCalls:    counter.structOldYoungRememberedMutations.Load(),
		ArrayOldYoungRememberedCalls:     counter.arrayOldYoungRememberedMutations.Load(),
		ArrayCardPresentCalls:            counter.arrayCardPresentMutations.Load(),
		ArrayCardCoveredCalls:            counter.arrayCardCoveredMutations.Load(),
	}
}
