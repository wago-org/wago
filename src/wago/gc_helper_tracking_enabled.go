//go:build wago_gcstats

package wago

import (
	"sync/atomic"

	"github.com/wago-org/wago/src/core/runtime/gc"
)

type gcHelperCounter struct {
	collector  *gc.Collector
	calls      atomic.Uint64
	allocCalls atomic.Uint64
	mutations  atomic.Uint64
}

var activeGCHelperCounter atomic.Pointer[gcHelperCounter]

func recordSynchronousGCHelper(collector *gc.Collector, helper uint32) {
	counter := activeGCHelperCounter.Load()
	if counter == nil || counter.collector != collector {
		return
	}
	counter.calls.Add(1)
	if gcHelperMayAllocate(helper) {
		counter.allocCalls.Add(1)
	}
	if gcHelperMayMutate(helper) {
		counter.mutations.Add(1)
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
		Calls:           counter.calls.Load(),
		AllocationCalls: counter.allocCalls.Load(),
		MutationCalls:   counter.mutations.Load(),
	}
}
