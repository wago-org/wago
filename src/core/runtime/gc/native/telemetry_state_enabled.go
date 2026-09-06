//go:build wago_gcstats

package gc

import (
	"math/bits"
	"time"
)

// Telemetry is reusable, bounded, and not concurrency-safe. Attach one recorder
// within one Collector enabled through Config.Telemetry. Serialize
// Snapshot/Reset with collector mutation.
type Telemetry struct {
	profile                 Profile
	allocationBaseline      uint64
	minor                   telemetryCollection
	full                    telemetryCollection
	active                  telemetryCycle
	paths                   PathTelemetry
	barriers                BarrierTelemetry
	pendingDuplicateDirties uint64
	occupancyHistogram      [11]uint64
	globalRootClasses       []uint8 // zero means RootGlobal; otherwise RootClass+1
}

type telemetryCollection struct {
	cycles  uint64
	failed  uint64
	totalNS uint64
	pause   telemetryHistogram
	phases  PhaseTelemetry
	roots   RootTelemetry
	trace   TraceTelemetry
	nursery NurseryTelemetry
	cards   CardTelemetry
}

type telemetryCycleKind uint8

const (
	telemetryMinor telemetryCycleKind = iota + 1
	telemetryFull
)

type telemetryPhase uint8

const (
	telemetryPhaseNone telemetryPhase = iota
	telemetryPhaseRootEnumeration
	telemetryPhasePersistentRoots
	telemetryPhaseNativeRoots
	telemetryPhaseRememberedRoots
	telemetryPhaseTracing
	telemetryPhaseMarking
	telemetryPhasePromotionCopy
	telemetryPhaseSweep
	telemetryPhaseFreeSpaceReconstruction
	telemetryPhaseFragmentationRecovery
	telemetryPhaseMetadataCleanup
	telemetryPhaseCount
)

type telemetryCycle struct {
	active         bool
	kind           telemetryCycleKind
	start          time.Time
	phaseStart     time.Time
	phase          telemetryPhase
	nestedScanNS   [telemetryPhaseCount]uint64
	phases         PhaseTelemetry
	roots          RootTelemetry
	trace          TraceTelemetry
	nursery        NurseryTelemetry
	cards          CardTelemetry
	rememberedScan bool
	suspendedPhase telemetryPhase
	suspendStart   time.Time
	suspendedNS    uint64
}

type telemetryHistogram struct {
	buckets [telemetryPauseBuckets]uint64
	count   uint64
	maxNS   uint64
}

func (h *telemetryHistogram) record(ns uint64) {
	h.buckets[telemetryPauseBucket(ns)]++
	h.count++
	if ns > h.maxNS {
		h.maxNS = ns
	}
}

func telemetryPauseBucket(ns uint64) int {
	if ns == 0 {
		return 0
	}
	exponent := bits.Len64(ns) - 1
	base := uint64(1) << exponent
	width := base / telemetryPauseSubBuckets
	if width == 0 {
		width = 1
	}
	sub := int((ns - base) / width)
	if sub >= telemetryPauseSubBuckets {
		sub = telemetryPauseSubBuckets - 1
	}
	return 1 + exponent*telemetryPauseSubBuckets + sub
}

func telemetryPauseBucketUpper(index int) uint64 {
	if index == 0 {
		return 0
	}
	index--
	exponent, sub := index/telemetryPauseSubBuckets, index%telemetryPauseSubBuckets
	base := uint64(1) << exponent
	width := base / telemetryPauseSubBuckets
	if width == 0 {
		width = 1
	}
	return base + uint64(sub+1)*width - 1
}

func (h *telemetryHistogram) percentile(numerator uint64) uint64 {
	if h.count == 0 {
		return 0
	}
	target := (h.count*numerator + 99) / 100
	var seen uint64
	for i, n := range h.buckets {
		seen += n
		if seen >= target {
			upper := telemetryPauseBucketUpper(i)
			if upper > h.maxNS {
				return h.maxNS
			}
			return upper
		}
	}
	return h.maxNS
}

func (h *telemetryHistogram) snapshot() PauseTelemetry {
	return PauseTelemetry{
		Count: h.count,
		P50NS: h.percentile(50),
		P90NS: h.percentile(90),
		P95NS: h.percentile(95),
		P99NS: h.percentile(99),
		MaxNS: h.maxNS,
	}
}

func (t *Telemetry) attach(profile Profile, allocations uint64) {
	if t == nil {
		return
	}
	*t = Telemetry{profile: profile, allocationBaseline: allocations}
}

func (t *Telemetry) setGlobalRootClass(index uint32, class RootClass) {
	if t == nil {
		return
	}
	for uint64(len(t.globalRootClasses)) <= uint64(index) {
		t.globalRootClasses = append(t.globalRootClasses, 0)
	}
	if class == RootGlobal {
		t.globalRootClasses[index] = 0
	} else {
		t.globalRootClasses[index] = uint8(class) + 1
	}
}

func (t *Telemetry) globalRootClass(index uint32) RootClass {
	if t == nil || !slotIndexOK(index, len(t.globalRootClasses)) || t.globalRootClasses[index] == 0 {
		return RootGlobal
	}
	class := RootClass(t.globalRootClasses[index] - 1)
	if class >= rootClassCount {
		return RootGlobal
	}
	return class
}

func (t *Telemetry) begin(kind telemetryCycleKind) {
	if t == nil {
		return
	}
	if t.active.active {
		// Nested forced-major collection is started only after the minor cycle is
		// committed, so an active cycle here indicates API misuse.
		t.end(false)
	}
	now := time.Now()
	t.active = telemetryCycle{active: true, kind: kind, start: now, phaseStart: now}
}

func (t *Telemetry) setPhase(phase telemetryPhase) {
	if t == nil || !t.active.active || !t.active.suspendStart.IsZero() || t.active.phase == phase {
		return
	}
	t.finishPhase(time.Now())
	t.active.phase = phase
	t.active.phaseStart = time.Now()
}

func (t *Telemetry) finishPhase(now time.Time) {
	c := &t.active
	if !c.active || c.phase == telemetryPhaseNone {
		return
	}
	duration := uint64(now.Sub(c.phaseStart))
	scan := c.nestedScanNS[c.phase]
	if scan > duration {
		scan = duration
	}
	outer := duration - scan
	addPhaseDuration(&c.phases, c.phase, outer)
	c.phases.ReferenceScanningNS += scan
	c.nestedScanNS[c.phase] = 0
}

func addPhaseDuration(p *PhaseTelemetry, phase telemetryPhase, ns uint64) {
	switch phase {
	case telemetryPhaseRootEnumeration:
		p.RootEnumerationNS += ns
	case telemetryPhasePersistentRoots:
		p.PersistentRootsNS += ns
	case telemetryPhaseNativeRoots:
		p.NativeFrameRootsNS += ns
	case telemetryPhaseRememberedRoots:
		p.RememberedRootsNS += ns
	case telemetryPhaseTracing:
		p.TracingNS += ns
	case telemetryPhaseMarking:
		p.MarkingNS += ns
	case telemetryPhasePromotionCopy:
		p.PromotionCopyNS += ns
	case telemetryPhaseSweep:
		p.SweepNS += ns
	case telemetryPhaseFreeSpaceReconstruction:
		p.FreeSpaceReconstructionNS += ns
	case telemetryPhaseFragmentationRecovery:
		p.FragmentationRecoveryNS += ns
	case telemetryPhaseMetadataCleanup:
		p.MetadataCleanupNS += ns
	}
}

func (t *Telemetry) end(success bool) {
	if t == nil || !t.active.active {
		return
	}
	now := time.Now()
	t.finishPhase(now)
	elapsed := uint64(now.Sub(t.active.start))
	if t.active.suspendedNS < elapsed {
		elapsed -= t.active.suspendedNS
	} else {
		elapsed = 0
	}
	var dst *telemetryCollection
	if t.active.kind == telemetryMinor {
		dst = &t.minor
	} else {
		dst = &t.full
	}
	dst.cycles++
	if !success {
		dst.failed++
	}
	dst.totalNS += elapsed
	dst.pause.record(elapsed)
	addPhaseTelemetry(&dst.phases, t.active.phases)
	addRootTelemetry(&dst.roots, t.active.roots)
	addTraceTelemetry(&dst.trace, t.active.trace)
	addNurseryTelemetry(&dst.nursery, t.active.nursery)
	addCardTelemetry(&dst.cards, t.active.cards)
	t.active = telemetryCycle{}
}

func addPhaseTelemetry(dst *PhaseTelemetry, src PhaseTelemetry) {
	dst.RootEnumerationNS += src.RootEnumerationNS
	dst.PersistentRootsNS += src.PersistentRootsNS
	dst.NativeFrameRootsNS += src.NativeFrameRootsNS
	dst.RememberedRootsNS += src.RememberedRootsNS
	dst.TracingNS += src.TracingNS
	dst.MarkingNS += src.MarkingNS
	dst.ReferenceScanningNS += src.ReferenceScanningNS
	dst.PromotionCopyNS += src.PromotionCopyNS
	dst.SweepNS += src.SweepNS
	dst.FreeSpaceReconstructionNS += src.FreeSpaceReconstructionNS
	dst.FragmentationRecoveryNS += src.FragmentationRecoveryNS
	dst.MetadataCleanupNS += src.MetadataCleanupNS
}

func addRootTelemetry(dst *RootTelemetry, src RootTelemetry) {
	dst.NativeFrames += src.NativeFrames
	dst.Globals += src.Globals
	dst.Tables += src.Tables
	dst.PublicTokens += src.PublicTokens
	dst.ForeignInstances += src.ForeignInstances
	dst.SnapshotTemporaries += src.SnapshotTemporaries
	dst.NativeFrameNS += src.NativeFrameNS
	dst.GlobalNS += src.GlobalNS
	dst.TableNS += src.TableNS
	dst.PublicTokenNS += src.PublicTokenNS
	dst.ForeignInstanceNS += src.ForeignInstanceNS
	dst.SnapshotTemporaryNS += src.SnapshotTemporaryNS
}

func addTraceTelemetry(dst *TraceTelemetry, src TraceTelemetry) {
	dst.ObjectsVisited += src.ObjectsVisited
	dst.PayloadBytesVisited += src.PayloadBytesVisited
	dst.ReferenceSlotsVisited += src.ReferenceSlotsVisited
	dst.ScanEntriesVisited += src.ScanEntriesVisited
	dst.ObjectScansBegun += src.ObjectScansBegun
	dst.ObjectScansResumed += src.ObjectScansResumed
	dst.ObjectScansCompleted += src.ObjectScansCompleted
	if src.MaxStepObjectRanges > dst.MaxStepObjectRanges {
		dst.MaxStepObjectRanges = src.MaxStepObjectRanges
	}
	if src.MaxStepScanEntries > dst.MaxStepScanEntries {
		dst.MaxStepScanEntries = src.MaxStepScanEntries
	}
	if src.MaxStepReferenceSlots > dst.MaxStepReferenceSlots {
		dst.MaxStepReferenceSlots = src.MaxStepReferenceSlots
	}
	if src.MaxStepPayloadBytes > dst.MaxStepPayloadBytes {
		dst.MaxStepPayloadBytes = src.MaxStepPayloadBytes
	}
	dst.ObjectsSwept += src.ObjectsSwept
	dst.PayloadBytesSwept += src.PayloadBytesSwept
}

func addNurseryTelemetry(dst *NurseryTelemetry, src NurseryTelemetry) {
	dst.AllocatedObjects += src.AllocatedObjects
	dst.AllocatedBytes += src.AllocatedBytes
	dst.SurvivedObjects += src.SurvivedObjects
	dst.SurvivedBytes += src.SurvivedBytes
	dst.PromotedObjects += src.PromotedObjects
	dst.PromotedBytes += src.PromotedBytes
	dst.CopiedBytes += src.CopiedBytes
	for i := range dst.AgeObjects {
		dst.AgeObjects[i] += src.AgeObjects[i]
		dst.AgeBytes[i] += src.AgeBytes[i]
		dst.PointerFreeAgeObjects[i] += src.PointerFreeAgeObjects[i]
		dst.PointerFreeAgeBytes[i] += src.PointerFreeAgeBytes[i]
	}
}

func addCardTelemetry(dst *CardTelemetry, src CardTelemetry) {
	dst.DirtyObjectCards += src.DirtyObjectCards
	dst.DirtyRootCards += src.DirtyRootCards
	dst.UsefulObjectCards += src.UsefulObjectCards
	dst.UsefulRootCards += src.UsefulRootCards
	dst.DuplicateDirties += src.DuplicateDirties
	dst.ScannedSlots += src.ScannedSlots
	dst.WholeObjectScans += src.WholeObjectScans
	dst.WholeObjectScansAvoided += src.WholeObjectScansAvoided
	dst.ClearedCards += src.ClearedCards
}

func (t *Telemetry) suspend() {
	if t == nil || !t.active.active || !t.active.suspendStart.IsZero() {
		return
	}
	now := time.Now()
	t.finishPhase(now)
	t.active.suspendedPhase = t.active.phase
	t.active.phase = telemetryPhaseNone
	t.active.suspendStart = now
}

func (t *Telemetry) resume() {
	if t == nil || !t.active.active || t.active.suspendStart.IsZero() {
		return
	}
	now := time.Now()
	t.active.suspendedNS += uint64(now.Sub(t.active.suspendStart))
	t.active.suspendStart = time.Time{}
	t.active.phase = t.active.suspendedPhase
	t.active.suspendedPhase = telemetryPhaseNone
	t.active.phaseStart = now
}

func (t *Telemetry) noteRoot(class RootClass) {
	if t == nil || !t.active.active || !t.active.suspendStart.IsZero() {
		return
	}
	switch class {
	case RootNativeFrame:
		t.active.roots.NativeFrames++
	case RootGlobal:
		t.active.roots.Globals++
	case RootTable:
		t.active.roots.Tables++
	case RootPublicToken:
		t.active.roots.PublicTokens++
	case RootForeignInstance:
		t.active.roots.ForeignInstances++
	case RootSnapshotTemporary:
		t.active.roots.SnapshotTemporaries++
	}
}

func (t *Telemetry) addRootTime(class RootClass, ns uint64) {
	if t == nil || !t.active.active || !t.active.suspendStart.IsZero() {
		return
	}
	switch class {
	case RootNativeFrame:
		t.active.roots.NativeFrameNS += ns
	case RootGlobal:
		t.active.roots.GlobalNS += ns
	case RootTable:
		t.active.roots.TableNS += ns
	case RootPublicToken:
		t.active.roots.PublicTokenNS += ns
	case RootForeignInstance:
		t.active.roots.ForeignInstanceNS += ns
	case RootSnapshotTemporary:
		t.active.roots.SnapshotTemporaryNS += ns
	}
}

func (t *Telemetry) scanStart() time.Time {
	if t == nil || !t.active.active || !t.active.suspendStart.IsZero() {
		return time.Time{}
	}
	return time.Now()
}

func (t *Telemetry) noteObjectScan(start time.Time, size, slots uint32) {
	work := objectScanWork{ObjectRanges: 1, RefSlots: slots}
	if size > PayloadOffset {
		work.PayloadBytes = size - PayloadOffset
	}
	t.noteObjectScanWork(start, work, true, false, true)
}

func (t *Telemetry) noteObjectScanWork(start time.Time, work objectScanWork, began, resumed, completed bool) {
	if t == nil || !t.active.active || !t.active.suspendStart.IsZero() {
		return
	}
	if began {
		t.active.trace.ObjectsVisited++
		t.active.trace.ObjectScansBegun++
	}
	if resumed {
		t.active.trace.ObjectScansResumed++
	}
	if completed {
		t.active.trace.ObjectScansCompleted++
	}
	t.active.trace.PayloadBytesVisited += uint64(work.PayloadBytes)
	t.active.trace.ReferenceSlotsVisited += uint64(work.RefSlots)
	t.active.trace.ScanEntriesVisited += uint64(work.ScanEntries)
	if t.active.rememberedScan {
		t.active.cards.ScannedSlots += uint64(work.RefSlots)
		if began && completed {
			t.active.cards.WholeObjectScans++
		}
	}
	if !start.IsZero() && t.active.phase < telemetryPhaseCount {
		t.active.nestedScanNS[t.active.phase] += uint64(time.Since(start))
	}
}

func (t *Telemetry) noteTinyStepWork(work objectScanWork) {
	if t == nil || !t.active.active || !t.active.suspendStart.IsZero() {
		return
	}
	trace := &t.active.trace
	if uint64(work.ObjectRanges) > trace.MaxStepObjectRanges {
		trace.MaxStepObjectRanges = uint64(work.ObjectRanges)
	}
	if uint64(work.ScanEntries) > trace.MaxStepScanEntries {
		trace.MaxStepScanEntries = uint64(work.ScanEntries)
	}
	if uint64(work.RefSlots) > trace.MaxStepReferenceSlots {
		trace.MaxStepReferenceSlots = uint64(work.RefSlots)
	}
	if uint64(work.PayloadBytes) > trace.MaxStepPayloadBytes {
		trace.MaxStepPayloadBytes = uint64(work.PayloadBytes)
	}
}

func (t *Telemetry) noteCardScan(start time.Time, payloadBytes, slots, dirtyCards, usefulCards uint64, whole bool) {
	if t == nil || !t.active.active || !t.active.suspendStart.IsZero() {
		return
	}
	t.active.trace.ObjectsVisited++
	t.active.trace.PayloadBytesVisited += payloadBytes
	t.active.trace.ReferenceSlotsVisited += slots
	t.active.cards.ScannedSlots += slots
	t.active.cards.UsefulObjectCards += usefulCards
	if whole {
		t.active.cards.WholeObjectScans++
	} else if dirtyCards != 0 {
		t.active.cards.WholeObjectScansAvoided++
	}
	if !start.IsZero() && t.active.phase < telemetryPhaseCount {
		t.active.nestedScanNS[t.active.phase] += uint64(time.Since(start))
	}
}

func (t *Telemetry) noteNurseryOccupancy(objects, bytes uint64) {
	if t == nil || !t.active.active || !t.active.suspendStart.IsZero() {
		return
	}
	t.active.nursery.AllocatedObjects += objects
	t.active.nursery.AllocatedBytes += bytes
}

func (t *Telemetry) noteSurvivor(size uint32, age uint8, pointerFree, copied bool) {
	if t == nil || !t.active.active || !t.active.suspendStart.IsZero() {
		return
	}
	bucket := int(age)
	if bucket >= len(t.active.nursery.AgeObjects) {
		bucket = len(t.active.nursery.AgeObjects) - 1
	}
	t.active.nursery.SurvivedObjects++
	t.active.nursery.SurvivedBytes += uint64(size)
	t.active.nursery.AgeObjects[bucket]++
	t.active.nursery.AgeBytes[bucket] += uint64(size)
	if pointerFree {
		t.active.nursery.PointerFreeAgeObjects[bucket]++
		t.active.nursery.PointerFreeAgeBytes[bucket] += uint64(size)
	}
	if copied {
		t.active.nursery.CopiedBytes += uint64(size)
	}
}

func (t *Telemetry) notePromotion(size uint32, copied bool) {
	if t == nil || !t.active.active || !t.active.suspendStart.IsZero() {
		return
	}
	t.active.nursery.PromotedObjects++
	t.active.nursery.PromotedBytes += uint64(size)
	if copied {
		t.active.nursery.CopiedBytes += uint64(size)
	}
}

func (t *Telemetry) noteSweep(size uint32) {
	if t == nil || !t.active.active || !t.active.suspendStart.IsZero() {
		return
	}
	t.active.trace.ObjectsSwept++
	if size > PayloadOffset {
		t.active.trace.PayloadBytesSwept += uint64(size - PayloadOffset)
	}
}

func (t *Telemetry) collectionSnapshot(src *telemetryCollection) CollectionTelemetry {
	return CollectionTelemetry{
		Cycles:       src.cycles,
		FailedCycles: src.failed,
		TotalNS:      src.totalNS,
		Pause:        src.pause.snapshot(),
		Phases:       src.phases,
		Roots:        src.roots,
		Trace:        src.trace,
		Nursery:      src.nursery,
		Cards:        src.cards,
	}
}

func (t *Telemetry) reset(profile Profile, allocations uint64) {
	if t == nil {
		return
	}
	classes := t.globalRootClasses
	*t = Telemetry{profile: profile, allocationBaseline: allocations, globalRootClasses: classes}
}
