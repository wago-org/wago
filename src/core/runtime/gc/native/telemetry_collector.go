//go:build wago_gcstats

package gc

import (
	"runtime"
	"unsafe"
)

func (c *Collector) beginCollectionTelemetry(kind telemetryCycleKind) {
	if !c.telemetryEnabled() {
		return
	}
	var nurseryObjects, nurseryBytes uint64
	for _, h := range c.nurseryHandles {
		if c.isYoungHandle(h) {
			nurseryObjects++
			nurseryBytes += uint64(c.handles[h].size)
		}
	}
	var usefulRootCards uint64
	for _, card := range c.slotCards {
		var r Ref
		switch card.kind {
		case SlotGlobal:
			if slotIndexOK(card.index, len(c.globalSlots)) {
				r = c.globalSlots[card.index]
			}
		case SlotTable:
			if slotIndexOK(card.index, len(c.tableSlots)) {
				r = c.tableSlots[card.index]
			}
		}
		if c.isNurseryRef(r) {
			usefulRootCards++
		}
	}
	c.cfg.Telemetry.begin(kind)
	if kind == telemetryMinor {
		c.cfg.Telemetry.paths.MinorCollections++
	} else {
		c.cfg.Telemetry.paths.FullCollections++
	}
	c.cfg.Telemetry.noteNurseryOccupancy(nurseryObjects, nurseryBytes)
	active := &c.cfg.Telemetry.active
	active.cards.DirtyObjectCards = c.dirtyObjectCardCount()
	active.cards.DirtyRootCards = uint64(len(c.slotCards))
	active.cards.UsefulRootCards = usefulRootCards
	active.cards.DuplicateDirties = c.cfg.Telemetry.pendingDuplicateDirties
	c.cfg.Telemetry.pendingDuplicateDirties = 0
}

func (c *Collector) endCollectionTelemetry(success bool) {
	if !c.telemetryEnabled() {
		return
	}
	c.cfg.Telemetry.end(success)
	heap := c.managedHeapTelemetry()
	bucket := uint64(0)
	if heap.CommittedBytes != 0 {
		bucket = heap.AllocatedBytes * 10 / heap.CommittedBytes
		if bucket > 10 {
			bucket = 10
		}
	}
	c.cfg.Telemetry.occupancyHistogram[bucket]++
}

// TelemetrySnapshot returns the current bounded telemetry state. The bool is
// false when telemetry was not enabled in Config.
func (c *Collector) TelemetrySnapshot() (TelemetrySnapshot, bool) {
	if !c.telemetryEnabled() {
		return TelemetrySnapshot{}, false
	}
	paths := c.cfg.Telemetry.paths
	allocations := c.stats.Allocations
	if allocations > c.cfg.Telemetry.allocationBaseline {
		observed := allocations - c.cfg.Telemetry.allocationBaseline
		if observed > paths.GoAllocationPaths {
			paths.NativeFastAllocations = observed - paths.GoAllocationPaths
		}
	}
	heap := c.managedHeapTelemetry()
	heap.OccupancyHistogram = c.cfg.Telemetry.occupancyHistogram
	var tiny TinyPolicyTelemetry
	if c.cfg.Profile == ProfileTiny {
		tiny = TinyPolicyTelemetry{
			IncrementalBuild:        tinyIncrementalBuild,
			AllocationDebtBytes:     uint64(c.tinyGC.allocationDebt),
			PacingStepLimit:         c.cfg.TinyPacingStepLimit,
			TransientRootLimit:      tinyTransientRootLimit,
			PersistentRootsPerStep:  tinyStepPersistentRoots,
			SweepHandlesPerStep:     tinyStepSweepHandles,
			SweepBlocksPerStep:      tinyStepSweepBlocks,
			SweepPoisonBytesPerStep: tinyStepSweepBytes,
			SweepBarrierWorkPending: c.tinyGC.rootPhase == tinyRootsSweepBarrier || (c.tinyGC.state == tinySweep && len(c.tinyGC.grayStack) != 0),
		}
	}
	return TelemetrySnapshot{
		SchemaVersion: TelemetrySchemaVersion,
		Profile:       c.cfg.Profile,
		Minor:         c.cfg.Telemetry.collectionSnapshot(&c.cfg.Telemetry.minor),
		Full:          c.cfg.Telemetry.collectionSnapshot(&c.cfg.Telemetry.full),
		Paths:         paths,
		Barriers:      c.cfg.Telemetry.barriers,
		Heap:          heap,
		Tiny:          tiny,
	}, true
}

// ResetTelemetry clears all counters and histograms without changing collector
// state. It returns false when telemetry is unavailable or an incremental cycle
// is active.
func (c *Collector) ResetTelemetry() bool {
	if !c.telemetryEnabled() || c.cfg.Telemetry.active.active {
		return false
	}
	c.cfg.Telemetry.reset(c.cfg.Profile, c.stats.Allocations)
	return true
}

// CaptureMemoryDomains samples the Go runtime heap and host peak RSS while
// preserving caller-attributed compiler and executable-JIT domains.
func CaptureMemoryDomains(compilerHeapBytes, executableJITBytes uint64, heap ManagedHeapTelemetry) MemoryDomains {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return MemoryDomains{
		GoCompilerHeapBytes: compilerHeapBytes,
		GoRuntimeHeapBytes:  mem.HeapAlloc,
		WasmManagedBytes:    heap.CommittedBytes,
		ExecutableJITBytes:  executableJITBytes,
		PeakRSSBytes:        peakRSSBytes(),
	}
}

func (c *Collector) managedHeapTelemetry() ManagedHeapTelemetry {
	if c == nil {
		return ManagedHeapTelemetry{}
	}
	var liveObjects, liveBytes, allocatedBytes uint64
	for h := uint32(1); int(h) < len(c.handles); h++ {
		e := c.handles[h]
		if e.space == spaceFree {
			continue
		}
		liveObjects++
		liveBytes += uint64(e.size)
		switch e.space {
		case spaceNursery:
			// Nursery physical occupancy is accounted from nurseryBump below so
			// alignment gaps are not reported as reusable free bytes.
		case spaceOld, spaceLarge:
			allocatedBytes += uint64(e.allocSize)
		case spaceTiny:
			if c.tiny.blockBytes != 0 {
				allocatedBytes += uint64(c.tiny.spanSize(e.off/c.tiny.blockBytes)) * uint64(c.tiny.blockBytes)
			}
		}
	}
	var committed, reserved, largest, spans uint64
	if c.cfg.Profile == ProfileTiny {
		committed = uint64(len(c.tiny.mem))
		reserved = committed
		for b := uint32(0); b < c.tiny.blockCount; {
			size := c.tiny.spanSize(b)
			if size == 0 || size > c.tiny.blockCount-b {
				break
			}
			if !c.tiny.isUsedStart(b) {
				bytes := uint64(size) * uint64(c.tiny.blockBytes)
				spans++
				if bytes > largest {
					largest = bytes
				}
			}
			b += size
		}
	} else {
		allocatedBytes += uint64(c.nurseryBump) + uint64(c.survivorBump)
		committed = uint64(len(c.nursery)) + uint64(len(c.throughput.mem))
		reserved = uint64(len(c.nursery)) + uint64(c.throughput.limit)
		for _, bytes := range []uint64{
			uint64(c.edenBytes() - c.nurseryBump),
			uint64(c.survivorBytes - c.survivorBump),
			uint64(c.survivorBytes), // inactive to-space
		} {
			if bytes == 0 {
				continue
			}
			spans++
			if bytes > largest {
				largest = bytes
			}
		}
		largestThroughput := uint64(c.throughput.largestFree())
		if largestThroughput > largest {
			largest = largestThroughput
		}
		spans += uint64(c.throughput.spanCount) + uint64(len(c.throughput.pendingFree))
		for _, pending := range c.throughput.pendingFree {
			if uint64(pending.size) > largest {
				largest = uint64(pending.size)
			}
		}
		if c.throughput.bump < uint32(len(c.throughput.mem)) {
			bytes := uint64(uint32(len(c.throughput.mem)) - c.throughput.bump)
			spans++
			if bytes > largest {
				largest = bytes
			}
		}
	}
	freeBytes := uint64(0)
	if committed > allocatedBytes {
		freeBytes = committed - allocatedBytes
	}
	fragmentation := uint64(0)
	if freeBytes > largest && freeBytes != 0 {
		fragmentation = (freeBytes - largest) * 1_000_000 / freeBytes
	}
	return ManagedHeapTelemetry{
		LiveObjects:      liveObjects,
		LiveBytes:        liveBytes,
		AllocatedBytes:   allocatedBytes,
		CommittedBytes:   committed,
		ReservedBytes:    reserved,
		FreeBytes:        freeBytes,
		LargestFreeBytes: largest,
		FreeSpanCount:    spans,
		MetadataBytes:    c.retainedMetadataBytes(),
		FragmentationPPM: fragmentation,
	}
}

func (c *Collector) retainedMetadataBytes() uint64 {
	if c == nil {
		return 0
	}
	bytes := uintptr(cap(c.handles))*unsafe.Sizeof(handleEntry{}) +
		uintptr(cap(c.freeHandles))*unsafe.Sizeof(uint32(0)) +
		uintptr(cap(c.nurseryHandles))*unsafe.Sizeof(uint32(0)) +
		uintptr(cap(c.mark))*unsafe.Sizeof(bool(false)) +
		uintptr(cap(c.markStack))*unsafe.Sizeof(uint32(0)) +
		uintptr(cap(c.promotionScratch))*unsafe.Sizeof(plannedPromotion{}) +
		uintptr(cap(c.remembered))*unsafe.Sizeof(uint32(0)) +
		uintptr(cap(c.objectCards))*unsafe.Sizeof(objectCard{}) +
		uintptr(cap(c.slotCards))*unsafe.Sizeof(slotCard{}) +
		uintptr(cap(c.globalCardBits)+cap(c.tableCardBits))*unsafe.Sizeof(uint64(0)) +
		uintptr(cap(c.globalSlots))*unsafe.Sizeof(Ref(0)) +
		uintptr(cap(c.tableSlots))*unsafe.Sizeof(Ref(0))
	if c.cfg.Profile == ProfileTiny {
		bytes += c.tiny.metadataBytes()
		bytes += uintptr(cap(c.tinyGC.color)) * unsafe.Sizeof(tinyMarkState(0))
		bytes += uintptr(cap(c.tinyGC.grayStack)) * unsafe.Sizeof(uint32(0))
	} else {
		bytes += uintptr(cap(c.throughput.spanNodes)) * unsafe.Sizeof(throughputSpanNode{})
		bytes += uintptr(cap(c.throughput.pendingFree)) * unsafe.Sizeof(throughputFreeSpan{})
	}
	return uint64(bytes)
}
