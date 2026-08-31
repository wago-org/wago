package gc

import (
	"errors"
	"fmt"
)

type tinyGCState uint8

const (
	tinyIdle tinyGCState = iota
	tinyMark
	tinyRemark
	tinySweep
)

type tinyRootPhase uint8

const (
	tinyRootsNone tinyRootPhase = iota
	tinyRootsTransient
	tinyRootsGlobals
	tinyRootsTables
	tinyRootsSweepBarrier
)

type tinyColor uint8

const (
	tinyWhite tinyColor = iota
	tinyGray
	tinyBlack
)

// tinyMarkState keeps one compact byte per handle. The low seven bits identify
// the cycle that marked the object, and the high bit distinguishes gray from
// black in that cycle. Any state from another epoch is logically white.
type tinyMarkState uint8

const (
	tinyMarkGrayBit   tinyMarkState = 1 << 7
	tinyMarkEpochMask uint8         = (1 << 7) - 1
)

func tinyEncodeMarkState(epoch uint8, color tinyColor) tinyMarkState {
	epoch &= tinyMarkEpochMask
	switch color {
	case tinyWhite:
		// Any non-current epoch is white. Use the immediately preceding epoch
		// so freshly grown metadata is deterministic around wraparound.
		return tinyMarkState((epoch + tinyMarkEpochMask) & tinyMarkEpochMask)
	case tinyGray:
		return tinyMarkState(epoch) | tinyMarkGrayBit
	case tinyBlack:
		return tinyMarkState(epoch)
	default:
		panic("gc: invalid Tiny color")
	}
}

const (
	// TinyStepBudget remains the allocation-time count of Step calls. Object
	// tracing inside each mark Step uses this independent fixed work vector.
	tinyStepObjectRanges    = uint32(64)
	tinyStepScanEntries     = uint32(256)
	tinyStepRefSlots        = uint32(256)
	tinyStepPayloadBytes    = uint32(1024)
	tinyStepPersistentRoots = uint32(256)
	tinyStepSweepHandles    = uint32(64)
	tinyStepSweepBlocks     = uint32(256)
	tinyStepSweepBytes      = uint32(4096)
	// Native/transient roots can change while the mutator runs, so they are
	// enumerated atomically at a safepoint rather than resumed across Steps.
	// Zero in telemetry means there is no semantic root-count limit; native frame
	// size and host resource policy bound the real work.
	tinyTransientRootLimit      = 0
	tinyAllocationDebtBytes     = uint32(1024)
	tinyNearExhaustionFactor    = uint32(8)
	tinyNearExhaustionStepLimit = uint32(32)
)

var tinyStepObjectScanBudget = objectScanBudget{
	ObjectRanges: tinyStepObjectRanges,
	ScanEntries:  tinyStepScanEntries,
	RefSlots:     tinyStepRefSlots,
	PayloadBytes: tinyStepPayloadBytes,
}

type tinyScanCursor struct {
	handle uint32
	scan   objectScanCursor
}

type tinyStepWork struct {
	objectRanges uint16
	scanEntries  uint16
	payloadBytes uint16
	refSlots     uint32
}

func makeTinyStepWork(work objectScanWork) tinyStepWork {
	return tinyStepWork{
		objectRanges: uint16(work.ObjectRanges),
		scanEntries:  uint16(work.ScanEntries),
		refSlots:     work.RefSlots,
		payloadBytes: uint16(work.PayloadBytes),
	}
}

func (work tinyStepWork) objectScanWork() objectScanWork {
	return objectScanWork{
		ObjectRanges: uint32(work.objectRanges),
		ScanEntries:  uint32(work.scanEntries),
		RefSlots:     work.refSlots,
		PayloadBytes: uint32(work.payloadBytes),
	}
}

type tinyGC struct {
	// Scalar fields precede slices so the cursor and per-Step work vector reuse
	// the former alignment padding; tinyGC retains its 64-bit footprint.
	state          tinyGCState
	telemetryOwned bool
	markEpoch      uint8
	rootPhase      tinyRootPhase
	// sweep indexes persistent roots during mark/remark and handles during sweep.
	sweep uint32
	// sweepLimit reuses the former cycle counter to preserve tinyGC's footprint.
	// It snapshots the finite handle-table endpoint when remark enters sweep;
	// allocations may append handles without extending the active cycle.
	sweepLimit     uint32
	allocationDebt uint32

	// At most one object scan is active. Its handle remains gray until scan is
	// complete; the cursor stores only stable handle/index state, never a raw
	// heap pointer. Step never exceeds tinyStepObjectScanBudget while marking.
	scan         tinyScanCursor
	lastStepWork tinyStepWork

	color     []tinyMarkState
	grayStack []uint32
}

// Step performs one Tiny incremental tri-color unit. Object tracing is bounded
// by tinyStepObjectScanBudget; root enumeration and sweep pacing are separate
// stages. When called while idle it starts a cycle by graying supplied roots.
func (c *Collector) Step(roots RootSet) error {
	if c.cfg.Profile != ProfileTiny {
		return c.CollectMinor(roots)
	}
	if err := c.errIfClosed(); err != nil {
		return err
	}
	if !tinyIncrementalBuild {
		return c.CollectFull(roots)
	}
	if collectorTelemetryEnabled {
		if c.tinyGC.state == tinyIdle && c.telemetryEnabled() && !c.cfg.Telemetry.active.active {
			c.beginCollectionTelemetry(telemetryFull)
			c.tinyGC.telemetryOwned = true
		}
		if c.tinyGC.telemetryOwned {
			// Incremental telemetry sums the bounded collector steps rather than
			// charging arbitrary mutator time between separate Step calls.
			c.cfg.Telemetry.resume()
			defer func() {
				if c.tinyGC.telemetryOwned {
					c.cfg.Telemetry.suspend()
				}
			}()
		}
	}
	c.tinyGC.lastStepWork = tinyStepWork{}
	if c.tinyGC.state == tinyIdle {
		if c.telemetryEnabled() {
			c.cfg.Telemetry.setPhase(telemetryPhaseRootEnumeration)
		}
		return c.tinyStartMark(roots)
	}
	if c.tinyGC.state == tinyMark {
		if c.tinyGC.rootPhase == tinyRootsSweepBarrier {
			if c.telemetryEnabled() {
				c.cfg.Telemetry.setPhase(telemetryPhaseMarking)
			}
			if c.tinyGC.scan.handle == 0 && len(c.tinyGC.grayStack) == 0 {
				c.tinyGC.state = tinySweep
				c.tinyGC.rootPhase = tinyRootsNone
				return nil
			}
			work := c.tinyDrainGrayBudget(tinyStepObjectScanBudget)
			c.tinyGC.lastStepWork = makeTinyStepWork(work)
			if c.telemetryEnabled() {
				c.cfg.Telemetry.noteTinyStepWork(work)
			}
			if c.tinyGC.scan.handle == 0 && len(c.tinyGC.grayStack) == 0 {
				c.tinyGC.state = tinySweep
				c.tinyGC.rootPhase = tinyRootsNone
			}
			return nil
		}
		if c.tinyGC.rootPhase != tinyRootsNone {
			if c.telemetryEnabled() {
				c.cfg.Telemetry.setPhase(telemetryPhaseRootEnumeration)
			}
			_, err := c.tinyDrainRootBudget(roots)
			return err
		}
		if c.telemetryEnabled() {
			c.cfg.Telemetry.setPhase(telemetryPhaseMarking)
		}
		if c.tinyGC.scan.handle == 0 && len(c.tinyGC.grayStack) == 0 {
			c.tinyGC.state = tinyRemark
			c.tinyGC.rootPhase = tinyRootsTransient
			c.tinyGC.sweep = 0
			return nil
		}
		work := c.tinyDrainGrayBudget(tinyStepObjectScanBudget)
		c.tinyGC.lastStepWork = makeTinyStepWork(work)
		if c.telemetryEnabled() {
			c.cfg.Telemetry.noteTinyStepWork(work)
		}
		return nil
	}
	if c.tinyGC.state == tinyRemark {
		if c.telemetryEnabled() {
			c.cfg.Telemetry.setPhase(telemetryPhaseRootEnumeration)
		}
		if c.tinyGC.rootPhase != tinyRootsNone {
			done, err := c.tinyDrainRootBudget(roots)
			if err != nil || !done {
				return err
			}
		}
		// A bulk barrier can perform bounded marking while the collector is in
		// remark and leave one object partially scanned with an empty gray stack.
		// Return to mark for either form of pending work; sweep must never begin
		// while an active cursor still owns a gray object.
		if c.tinyGC.scan.handle != 0 || len(c.tinyGC.grayStack) > 0 {
			c.tinyGC.state = tinyMark
			return nil
		}
		c.tinyGC.state = tinySweep
		c.tinyGC.sweep = 1
		c.tinyGC.sweepLimit = uint32(len(c.handles))
		return nil
	}
	if c.telemetryEnabled() {
		c.cfg.Telemetry.setPhase(telemetryPhaseSweep)
	}
	return c.tinySweepBudget()
}

func (c *Collector) tinySweepBudget() error {
	limit := c.tinyGC.sweepLimit
	if limit == 0 || uint64(limit) > uint64(len(c.handles)) {
		return c.failTinyTelemetryCycle(fmt.Errorf("gc: invalid Tiny sweep limit %d for %d handles", limit, len(c.handles)))
	}
	var handles, blocks uint32
	for handles < tinyStepSweepHandles {
		if c.tinyGC.scan.handle == 0 && len(c.tinyGC.grayStack) != 0 {
			c.tinyGC.state = tinyMark
			c.tinyGC.rootPhase = tinyRootsSweepBarrier
			return nil
		}
		if c.tinyGC.scan.handle != 0 {
			h := c.tinyGC.scan.handle
			if h != c.tinyGC.sweep || int(h) >= len(c.handles) || c.handles[h].space != spaceTiny || c.tinyColorOf(h) != tinyWhite {
				return c.failTinyTelemetryCycle(fmt.Errorf("gc: invalid Tiny sweep poison cursor for handle %d", h))
			}
			block := c.handles[h].off / c.tiny.blockBytes
			span := c.tiny.spanSize(block)
			totalBytes := uint64(span) * uint64(c.tiny.blockBytes)
			start := uint64(c.tinyGC.scan.scan.index)
			if span == 0 || start >= totalBytes {
				return c.failTinyTelemetryCycle(fmt.Errorf("gc: invalid Tiny sweep poison offset %d/%d", start, totalBytes))
			}
			n := totalBytes - start
			if n > uint64(tinyStepSweepBytes) {
				n = uint64(tinyStepSweepBytes)
			}
			lo := uint64(c.handles[h].off) + start
			hi := lo + n
			for i := range c.tiny.mem[lo:hi] {
				c.tiny.mem[lo+uint64(i)] = 0xdd
			}
			c.tinyGC.scan.scan.index += uint32(n)
			if uint64(c.tinyGC.scan.scan.index) < totalBytes {
				return nil
			}
			if c.telemetryEnabled() {
				c.cfg.Telemetry.noteSweep(c.handles[h].size)
			}
			c.tinyGC.scan = tinyScanCursor{}
			c.tinyGC.sweep++
			c.freeTinyPrepoisoned(h)
			if len(c.tinyGC.grayStack) != 0 {
				c.tinyGC.state = tinyMark
				c.tinyGC.rootPhase = tinyRootsSweepBarrier
			}
			return nil
		}
		if c.tinyGC.sweep >= limit {
			c.tinyFinishCycle()
			return nil
		}
		h := c.tinyGC.sweep
		handles++
		if c.handles[h].space != spaceTiny {
			c.tinyGC.sweep++
			continue
		}
		block := c.handles[h].off / c.tiny.blockBytes
		span := c.tiny.spanSize(block)
		if span == 0 {
			return c.failTinyTelemetryCycle(fmt.Errorf("gc: Tiny sweep handle %d has no allocation span", h))
		}
		if blocks != 0 && blocks+span > tinyStepSweepBlocks {
			return nil
		}
		blocks += span
		switch c.tinyColorOf(h) {
		case tinyWhite:
			if c.tiny.poisonFreed && uint64(span)*uint64(c.tiny.blockBytes) > uint64(tinyStepSweepBytes) {
				c.tinyGC.scan = tinyScanCursor{handle: h}
				continue
			}
			if c.telemetryEnabled() {
				c.cfg.Telemetry.noteSweep(c.handles[h].size)
			}
			c.tinyGC.sweep++
			c.free(h)
		case tinyBlack:
			// Survivors retain their current-epoch black state. Advancing the
			// epoch at the next cycle start makes them white in O(1).
			c.tinyGC.sweep++
		case tinyGray:
			return c.failTinyTelemetryCycle(fmt.Errorf("gc: gray object %d reached tiny sweep", h))
		default:
			return c.failTinyTelemetryCycle(fmt.Errorf("gc: invalid tiny color for handle %d", h))
		}
		if blocks >= tinyStepSweepBlocks {
			return nil
		}
	}
	return nil
}

func (c *Collector) failTinyTelemetryCycle(err error) error {
	if c.tinyGC.telemetryOwned {
		c.endCollectionTelemetry(false)
		c.tinyGC.telemetryOwned = false
	}
	return err
}

func (c *Collector) tinyCollectFull(roots RootSet) error {
	if !tinyIncrementalBuild {
		return c.tinyCollectNonIncremental(roots)
	}
	if err := c.tinyStartMark(roots); err != nil {
		return err
	}
	for c.tinyGC.state != tinyIdle {
		if err := c.Step(roots); err != nil {
			return err
		}
	}
	return nil
}

func (c *Collector) tinyCollectNonIncremental(roots RootSet) error {
	if err := c.tinyCountTransientRoots(roots); err != nil {
		return err
	}
	c.tinyGC.markEpoch = (c.tinyGC.markEpoch + 1) & tinyMarkEpochMask
	c.tinyGC.sweepLimit = 0
	c.tinyGC.grayStack = c.tinyGC.grayStack[:0]
	c.tinyGC.scan = tinyScanCursor{}
	c.tinyGC.rootPhase = tinyRootsNone
	c.tinyGC.state = tinyMark
	if err := c.tinyMarkTransientRoots(roots); err != nil {
		return err
	}
	for _, r := range c.globalSlots {
		c.tinyMarkRef(r)
	}
	for _, r := range c.tableSlots {
		c.tinyMarkRef(r)
	}
	for c.tinyGC.scan.handle != 0 || len(c.tinyGC.grayStack) != 0 {
		if work := c.tinyDrainGrayBudget(completeObjectScanBudget); work == (objectScanWork{}) {
			return errors.New("gc: Tiny nonincremental mark made no progress")
		}
	}
	c.tinyGC.state = tinySweep
	c.tinyGC.sweep = 1
	for h := uint32(1); int(h) < len(c.handles); h++ {
		if c.handles[h].space != spaceTiny {
			continue
		}
		switch c.tinyColorOf(h) {
		case tinyWhite:
			if c.telemetryEnabled() {
				c.cfg.Telemetry.noteSweep(c.handles[h].size)
			}
			c.free(h)
		case tinyBlack:
		case tinyGray:
			return fmt.Errorf("gc: gray object %d reached Tiny nonincremental sweep", h)
		default:
			return fmt.Errorf("gc: invalid Tiny color for handle %d", h)
		}
	}
	c.tinyFinishCycle()
	return nil
}

func (c *Collector) tinyStartMark(roots RootSet) error {
	if c.tinyGC.state == tinySweep && c.tinyGC.scan.handle != 0 {
		return errors.New("gc: Tiny bounded poison sweep must complete before collection restart")
	}
	if err := c.tinyCountTransientRoots(roots); err != nil {
		return c.failTinyTelemetryCycle(err)
	}
	// Advancing to a fresh epoch makes every previously live object logically
	// white without walking the handle table. Seven epoch bits are intentional:
	// restarting CollectFull during an active cycle advances to a third value, so
	// neither current marks nor the preceding white population can alias black.
	c.tinyGC.markEpoch = (c.tinyGC.markEpoch + 1) & tinyMarkEpochMask
	c.tinyGC.sweepLimit = 0
	c.tinyGC.grayStack = c.tinyGC.grayStack[:0]
	c.tinyGC.scan = tinyScanCursor{}
	c.tinyGC.lastStepWork = tinyStepWork{}
	c.tinyGC.state = tinyMark
	c.tinyGC.rootPhase = tinyRootsTransient
	c.tinyGC.sweep = 0
	if err := c.tinyMarkTransientRoots(roots); err != nil {
		return c.failTinyTelemetryCycle(err)
	}
	c.tinyGC.rootPhase = tinyRootsGlobals
	_, err := c.tinyDrainRootBudget(nil)
	return err
}

func (c *Collector) tinyWalkTransientRoots(roots RootSet) (bool, error) {
	return c.tinyWalkTransientRootSet(roots, RootNativeFrame)
}

func (c *Collector) tinyWalkTransientRootSet(roots RootSet, class RootClass) (bool, error) {
	if roots == nil {
		return true, nil
	}
	switch roots := roots.(type) {
	case *ArrayInitializerRootScratch:
		if roots == nil {
			return true, nil
		}
		if complete, err := c.tinyWalkTransientRootSet(roots.first, class); err != nil || !complete {
			return complete, err
		}
		switch roots.mode {
		case 1:
			return c.tinyVisitTransientRoot(class, roots.uniform), nil
		case 2:
			for i := range roots.values {
				if !c.tinyVisitTransientRoot(class, roots.values[i].Ref) {
					return false, nil
				}
			}
		}
		return true, nil
	case *InitializerRootScratch:
		if roots == nil {
			return true, nil
		}
		if complete, err := c.tinyWalkTransientRootSet(roots.first, class); err != nil || !complete {
			return complete, err
		}
		for i := range roots.values {
			if i < len(roots.fields) && isCollectorRefKind(roots.fields[i].Kind) && !c.tinyVisitTransientRoot(class, roots.values[i].Ref) {
				return false, nil
			}
		}
		return true, nil
	case *InitializerWordRootScratch:
		if roots == nil {
			return true, nil
		}
		if complete, err := c.tinyWalkTransientRootSet(roots.first, class); err != nil || !complete {
			return complete, err
		}
		cursor := 0
		for _, field := range roots.fields {
			if cursor >= len(roots.words) {
				break
			}
			if isCollectorRefKind(field.Kind) && !c.tinyVisitTransientRoot(class, Ref(uint32(roots.words[cursor]))) {
				return false, nil
			}
			cursor++
			if field.Kind == StorageV128 {
				cursor++
			}
		}
		return true, nil
	case combinedRootSet:
		if complete, err := c.tinyWalkTransientRootSet(roots.first, class); err != nil || !complete {
			return complete, err
		}
		return c.tinyWalkTransientRootSet(roots.second, class)
	case extraRootSet:
		if complete, err := c.tinyWalkTransientRootSet(roots.roots, class); err != nil || !complete {
			return complete, err
		}
		if roots.extra != nil {
			return c.tinyVisitTransientRoot(class, roots.extra.GetRef()), nil
		}
		return true, nil
	case *RootGroups:
		if roots == nil {
			return true, nil
		}
		return c.tinyWalkTransientRootSet(RootGroups(*roots), class)
	case *ClassifiedRoots:
		if roots == nil {
			return true, nil
		}
		return c.tinyWalkTransientRootSet(ClassifiedRoots(*roots), class)
	case RootGroups:
		for _, group := range roots {
			complete, err := c.tinyWalkTransientRootSet(group.Roots, group.Class)
			if err != nil || !complete {
				return complete, err
			}
		}
		return true, nil
	case ClassifiedRoots:
		return c.tinyWalkTransientRootSet(roots.Roots, roots.Class)
	}
	if c.telemetryEnabled() {
		if classified, ok := roots.(DirectClassifiedRootRefSet); ok {
			return classified.RangeClassifiedRootRefs(c), nil
		}
	}
	if direct, ok := roots.(DirectRootRefSet); ok {
		previous := c.telemetryRootClass
		c.telemetryRootClass = class
		complete := direct.RangeRootRefs(c)
		c.telemetryRootClass = previous
		return complete, nil
	}
	if classified, ok := roots.(DirectClassifiedRootRefSet); ok {
		return classified.RangeClassifiedRootRefs(c), nil
	}
	return false, errors.New("gc: Tiny transient roots require bounded direct enumeration")
}

func (c *Collector) tinyVisitTransientRoot(class RootClass, r Ref) bool {
	if c.telemetryEnabled() {
		return c.VisitClassifiedRootRef(class, r)
	}
	return c.VisitRootRef(r)
}

func (c *Collector) tinyCountTransientRoots(roots RootSet) error {
	c.rootMarkMode = rootMarkTinyCount
	c.tinyGC.lastStepWork.refSlots = 0
	complete, err := c.tinyWalkTransientRoots(roots)
	c.finishDirectRootMark()
	c.tinyGC.lastStepWork.refSlots = 0
	if err != nil {
		return err
	}
	if !complete {
		return errors.New("gc: Tiny transient root enumeration stopped unexpectedly")
	}
	return nil
}

func (c *Collector) tinyMarkTransientRoots(roots RootSet) error {
	c.rootMarkMode = rootMarkTinyBounded
	c.tinyGC.lastStepWork.refSlots = 0
	complete, err := c.tinyWalkTransientRoots(roots)
	c.finishDirectRootMark()
	c.tinyGC.lastStepWork.refSlots = 0
	if err != nil {
		return err
	}
	if !complete {
		return errors.New("gc: Tiny transient root set changed during bounded enumeration")
	}
	return nil
}

func (c *Collector) tinyDrainRootBudget(roots RootSet) (bool, error) {
	if c.tinyGC.rootPhase == tinyRootsTransient {
		if err := c.tinyCountTransientRoots(roots); err != nil {
			return false, c.failTinyTelemetryCycle(err)
		}
		if err := c.tinyMarkTransientRoots(roots); err != nil {
			return false, c.failTinyTelemetryCycle(err)
		}
		c.tinyGC.rootPhase = tinyRootsGlobals
		c.tinyGC.sweep = 0
	}

	remaining := tinyStepPersistentRoots
	if c.telemetryEnabled() {
		c.rootMarkMode = rootMarkTiny
		defer c.finishDirectRootMark()
	}
	for remaining != 0 && c.tinyGC.rootPhase != tinyRootsNone {
		switch c.tinyGC.rootPhase {
		case tinyRootsGlobals:
			if c.tinyGC.sweep >= uint32(len(c.globalSlots)) {
				c.tinyGC.rootPhase = tinyRootsTables
				c.tinyGC.sweep = 0
				continue
			}
			i := c.tinyGC.sweep
			c.tinyGC.sweep++
			r := c.globalSlots[i]
			if c.telemetryEnabled() {
				c.VisitClassifiedRootRef(c.cfg.Telemetry.globalRootClass(i), r)
			} else {
				c.tinyMarkRef(r)
			}
			remaining--
		case tinyRootsTables:
			if c.tinyGC.sweep >= uint32(len(c.tableSlots)) {
				c.tinyGC.rootPhase = tinyRootsNone
				c.tinyGC.sweep = 1
				continue
			}
			i := c.tinyGC.sweep
			c.tinyGC.sweep++
			r := c.tableSlots[i]
			if c.telemetryEnabled() {
				c.VisitClassifiedRootRef(RootTable, r)
			} else {
				c.tinyMarkRef(r)
			}
			remaining--
		default:
			return false, c.failTinyTelemetryCycle(fmt.Errorf("gc: invalid Tiny root phase %d", c.tinyGC.rootPhase))
		}
	}
	return c.tinyGC.rootPhase == tinyRootsNone, nil
}

func (c *Collector) tinyFinishCycle() {
	c.tinyGC.grayStack = c.tinyGC.grayStack[:0]
	c.tinyGC.scan = tinyScanCursor{}
	c.tinyGC.lastStepWork = tinyStepWork{}
	c.tinyGC.state = tinyIdle
	c.tinyGC.rootPhase = tinyRootsNone
	c.tinyGC.sweep = 1
	c.tinyGC.sweepLimit = 0
	if c.tinyGC.telemetryOwned {
		c.cfg.Telemetry.setPhase(telemetryPhaseMetadataCleanup)
		c.endCollectionTelemetry(true)
		c.tinyGC.telemetryOwned = false
	}
}

func (c *Collector) tinyMarkRef(r Ref) {
	if !r.IsObj() {
		return
	}
	h := handleOf(r)
	if h == 0 || int(h) >= len(c.handles) || c.handles[h].space != spaceTiny {
		return
	}
	if !c.tinyIsWhite(h) {
		return
	}
	c.tinyQueueGrayHandle(h)
}

func (c *Collector) tinySweepActive() bool {
	return c.tinyGC.state == tinySweep || c.tinyGC.rootPhase == tinyRootsSweepBarrier
}

func (c *Collector) tinyMarkSweepRef(r Ref) {
	if c.tinyGC.state != tinySweep {
		c.tinyMarkRef(r)
		return
	}
	if c.tinyGC.scan.handle != 0 {
		// Debug poison owns the shared compact cursor. Queue root work alongside
		// it; sweep yields to the fixed mark budget before advancing another handle.
		c.tinyMarkRef(r)
		return
	}
	before := len(c.tinyGC.grayStack)
	c.tinyMarkRef(r)
	if len(c.tinyGC.grayStack) != before {
		c.tinyGC.state = tinyMark
		c.tinyGC.rootPhase = tinyRootsSweepBarrier
	}
}

// tinyDrainGrayBudget consumes a bounded vector of object-scan work. The active
// object always resumes before newly discovered gray objects, preserving the
// complete scanner's descriptor/element visitation order. An active object
// remains gray and is not present on grayStack; it becomes black only after its
// final outgoing reference has been visited.
func (c *Collector) tinyDrainGrayBudget(budget objectScanBudget) (used objectScanWork) {
	for {
		remaining := budget.remaining(used)
		if remaining.ObjectRanges == 0 {
			return used
		}
		began := false
		if c.tinyGC.scan.handle == 0 {
			for len(c.tinyGC.grayStack) > 0 {
				n := len(c.tinyGC.grayStack) - 1
				h := c.tinyGC.grayStack[n]
				c.tinyGC.grayStack = c.tinyGC.grayStack[:n]
				if int(h) >= len(c.handles) || c.handles[h].space != spaceTiny || c.tinyColorOf(h) != tinyGray {
					continue
				}
				c.tinyGC.scan.handle = h
				began = true
				break
			}
			if c.tinyGC.scan.handle == 0 {
				return used
			}
		}

		h := c.tinyGC.scan.handle
		var work objectScanWork
		var complete bool
		if c.telemetryEnabled() {
			start := c.cfg.Telemetry.scanStart()
			work, complete = c.scanObjectRefsRange(h, &c.tinyGC.scan.scan, remaining, objectScanVisitor{mode: objectScanVisitTinyMark})
			telemetryWork := work
			telemetryWork.PayloadBytes = 0
			if complete {
				// Preserve PayloadBytesVisited's pre-cursor meaning: account the
				// complete logical payload once when an object scan completes. The
				// per-Step maximum still uses work.PayloadBytes below.
				telemetryWork.PayloadBytes = c.objectPayloadBytes(h)
			}
			c.cfg.Telemetry.noteObjectScanWork(start, telemetryWork, began, !began, complete)
		} else {
			work, complete = c.scanObjectRefsRange(h, &c.tinyGC.scan.scan, remaining, objectScanVisitor{mode: objectScanVisitTinyMark})
		}
		used.add(work)
		if complete {
			if int(h) < len(c.handles) && c.handles[h].space == spaceTiny && c.tinyIsGray(h) {
				c.tinySetBlack(h)
			}
			c.tinyGC.scan = tinyScanCursor{}
			continue
		}
		return used
	}
}

func (c *Collector) tinyGrayHandle(h uint32) {
	if c.tinyIsGray(h) {
		return
	}
	c.tinyQueueGrayHandle(h)
}

func (c *Collector) tinyQueueGrayHandle(h uint32) {
	c.tinySetGray(h)
	c.tinyGC.grayStack = append(c.tinyGC.grayStack, h)
}

func (c *Collector) tinyColorOf(h uint32) tinyColor {
	if int(h) >= len(c.tinyGC.color) {
		return tinyWhite
	}
	state := c.tinyGC.color[h]
	black := tinyMarkState(c.tinyGC.markEpoch)
	if state == black {
		return tinyBlack
	}
	if state == black|tinyMarkGrayBit {
		return tinyGray
	}
	return tinyWhite
}

func (c *Collector) tinyIsWhite(h uint32) bool {
	if int(h) >= len(c.tinyGC.color) {
		return true
	}
	state := c.tinyGC.color[h]
	black := tinyMarkState(c.tinyGC.markEpoch)
	return state != black && state != black|tinyMarkGrayBit
}

func (c *Collector) tinyIsGray(h uint32) bool {
	return int(h) < len(c.tinyGC.color) && c.tinyGC.color[h] == tinyMarkState(c.tinyGC.markEpoch)|tinyMarkGrayBit
}

// These setters are deliberately branch-free. Published Tiny handles always
// have mark metadata; keeping the hot barrier/mark path direct avoids imposing
// generic color encoding and metadata-growth checks on every shade operation.
func (c *Collector) tinySetBlack(h uint32) { c.tinyGC.color[h] = tinyMarkState(c.tinyGC.markEpoch) }
func (c *Collector) tinySetGray(h uint32) {
	c.tinyGC.color[h] = tinyMarkState(c.tinyGC.markEpoch) | tinyMarkGrayBit
}
func (c *Collector) tinySetWhite(h uint32) {
	c.tinyGC.color[h] = tinyMarkState((c.tinyGC.markEpoch + tinyMarkEpochMask) & tinyMarkEpochMask)
}

func (c *Collector) tinySetColor(h uint32, color tinyColor) {
	switch color {
	case tinyWhite:
		c.tinySetWhite(h)
	case tinyGray:
		c.tinySetGray(h)
	case tinyBlack:
		c.tinySetBlack(h)
	default:
		panic("gc: invalid Tiny color")
	}
}

func (c *Collector) verifyTiny(roots RootSet) error {
	if c.tinyGC.state > tinySweep {
		return fmt.Errorf("gc: invalid tiny collector state %d", c.tinyGC.state)
	}
	if c.tinyGC.markEpoch > tinyMarkEpochMask {
		return fmt.Errorf("gc: invalid tiny mark epoch %d", c.tinyGC.markEpoch)
	}
	if c.tinyGC.rootPhase > tinyRootsSweepBarrier {
		return fmt.Errorf("gc: invalid Tiny root phase %d", c.tinyGC.rootPhase)
	}
	switch c.tinyGC.rootPhase {
	case tinyRootsNone:
		if (c.tinyGC.state == tinyIdle || c.tinyGC.state == tinySweep) && c.tinyGC.sweep == 0 {
			return fmt.Errorf("gc: Tiny state %d has zero sweep cursor", c.tinyGC.state)
		}
	case tinyRootsTransient:
		if c.tinyGC.state != tinyMark && c.tinyGC.state != tinyRemark {
			return fmt.Errorf("gc: transient Tiny roots active in state %d", c.tinyGC.state)
		}
		if c.tinyGC.sweep != 0 {
			return fmt.Errorf("gc: transient Tiny root cursor = %d, want zero", c.tinyGC.sweep)
		}
	case tinyRootsGlobals:
		if c.tinyGC.state != tinyMark && c.tinyGC.state != tinyRemark || c.tinyGC.sweep > uint32(len(c.globalSlots)) {
			return fmt.Errorf("gc: invalid Tiny global-root cursor %d in state %d", c.tinyGC.sweep, c.tinyGC.state)
		}
	case tinyRootsTables:
		if c.tinyGC.state != tinyMark && c.tinyGC.state != tinyRemark || c.tinyGC.sweep > uint32(len(c.tableSlots)) {
			return fmt.Errorf("gc: invalid Tiny table-root cursor %d in state %d", c.tinyGC.sweep, c.tinyGC.state)
		}
	case tinyRootsSweepBarrier:
		if c.tinyGC.state != tinyMark || c.tinyGC.sweep == 0 {
			return fmt.Errorf("gc: invalid Tiny sweep-barrier cursor %d in state %d", c.tinyGC.sweep, c.tinyGC.state)
		}
	}
	if (c.tinyGC.state == tinyIdle || c.tinyGC.state == tinySweep) && c.tinyGC.rootPhase != tinyRootsNone {
		return fmt.Errorf("gc: Tiny state %d retains root phase %d", c.tinyGC.state, c.tinyGC.rootPhase)
	}
	sweepActive := c.tinyGC.state == tinySweep || c.tinyGC.rootPhase == tinyRootsSweepBarrier
	if sweepActive {
		if c.tinyGC.sweepLimit == 0 || uint64(c.tinyGC.sweepLimit) > uint64(len(c.handles)) || c.tinyGC.sweep > c.tinyGC.sweepLimit {
			return fmt.Errorf("gc: invalid Tiny sweep cursor/limit %d/%d for %d handles", c.tinyGC.sweep, c.tinyGC.sweepLimit, len(c.handles))
		}
	} else if c.tinyGC.sweepLimit != 0 {
		return fmt.Errorf("gc: Tiny state %d retains sweep limit %d", c.tinyGC.state, c.tinyGC.sweepLimit)
	}
	if len(c.tinyGC.color) < len(c.handles) {
		return fmt.Errorf("gc: tiny mark metadata has %d entries for %d handles", len(c.tinyGC.color), len(c.handles))
	}
	if work := c.tinyGC.lastStepWork.objectScanWork(); work.ObjectRanges > tinyStepObjectRanges || work.ScanEntries > tinyStepScanEntries || work.RefSlots > tinyStepRefSlots || work.PayloadBytes > tinyStepPayloadBytes {
		return fmt.Errorf("gc: Tiny Step work exceeds bound: %+v", work)
	}
	if c.tiny.blockBytes == 0 || len(c.tiny.mem) == 0 {
		return errors.New("gc: tiny heap is not initialized")
	}
	if err := c.tiny.verifyMetadataShape(); err != nil {
		return err
	}
	liveBlocks := make([]bool, c.tiny.blockCount)
	for h := uint32(1); int(h) < len(c.handles); h++ {
		e := c.handles[h]
		if e.space == spaceFree {
			continue
		}
		if e.space != spaceTiny {
			return fmt.Errorf("gc: non-tiny handle %d in tiny collector", h)
		}
		memBytes := uint32(len(c.tiny.mem))
		if e.off%c.tiny.blockBytes != 0 || e.off > memBytes || e.size > memBytes-e.off {
			return fmt.Errorf("gc: tiny handle %d out of bounds", h)
		}
		bi := e.off / c.tiny.blockBytes
		if bi >= c.tiny.blockCount || !c.tiny.isUsedStart(bi) {
			return fmt.Errorf("gc: tiny handle %d points to free span", h)
		}
		spanBlocks := c.tiny.spanSize(bi)
		if spanBlocks == 0 || spanBlocks > c.tiny.blockCount-bi {
			return fmt.Errorf("gc: tiny handle %d has invalid allocation span", h)
		}
		spanBytes := spanBlocks * c.tiny.blockBytes
		if e.size > spanBytes {
			return fmt.Errorf("gc: tiny handle %d exceeds allocation span", h)
		}
		for i := bi; i < bi+spanBlocks; i++ {
			if int(i) >= len(liveBlocks) || liveBlocks[i] {
				return fmt.Errorf("gc: tiny live span overlap at block %d", i)
			}
			liveBlocks[i] = true
		}
		if col := c.tinyColorOf(h); c.tinyGC.state == tinyIdle && col != tinyBlack {
			return fmt.Errorf("gc: idle tiny handle %d has color %d, want black in epoch %d", h, col, c.tinyGC.markEpoch)
		}
	}
	seenGray := make([]bool, len(c.handles))
	for _, h := range c.tinyGC.grayStack {
		if h == 0 || int(h) >= len(c.handles) || c.handles[h].space != spaceTiny || c.tinyColorOf(h) != tinyGray {
			return fmt.Errorf("gc: invalid tiny gray-stack handle %d", h)
		}
		if seenGray[h] {
			return fmt.Errorf("gc: duplicate tiny gray-stack handle %d", h)
		}
		seenGray[h] = true
	}
	if active := c.tinyGC.scan.handle; active != 0 {
		if c.tinyGC.state == tinySweep {
			if !c.tiny.poisonFreed || active != c.tinyGC.sweep || int(active) >= len(c.handles) || c.handles[active].space != spaceTiny || c.tinyColorOf(active) != tinyWhite {
				return fmt.Errorf("gc: invalid active Tiny sweep cursor handle %d", active)
			}
			block := c.handles[active].off / c.tiny.blockBytes
			span := c.tiny.spanSize(block)
			totalBytes := uint64(span) * uint64(c.tiny.blockBytes)
			if span == 0 || c.tinyGC.scan.scan.index == 0 || uint64(c.tinyGC.scan.scan.index) >= totalBytes {
				return fmt.Errorf("gc: invalid active Tiny sweep cursor %d/%d", c.tinyGC.scan.scan.index, totalBytes)
			}
		} else {
			if c.tinyGC.state != tinyMark && c.tinyGC.state != tinyRemark {
				return fmt.Errorf("gc: active tiny scan in state %d", c.tinyGC.state)
			}
			if int(active) >= len(c.handles) || c.handles[active].space != spaceTiny || c.tinyColorOf(active) != tinyGray || seenGray[active] {
				return fmt.Errorf("gc: invalid active tiny scan handle %d", active)
			}
			r := makeObjRef(active)
			d, err := c.refDesc(r)
			if err != nil || !d.HasRefs {
				return fmt.Errorf("gc: active tiny scan handle %d has no reference layout", active)
			}
			limit := uint32(len(d.Fields))
			if d.Kind == KindArray {
				if !d.ArrayElementsAreRefs() {
					return fmt.Errorf("gc: active tiny array scan handle %d has non-reference elements", active)
				}
				limit = c.header(r).Aux
			}
			if c.tinyGC.scan.scan.index >= limit {
				return fmt.Errorf("gc: active tiny scan handle %d cursor %d beyond %d entries", active, c.tinyGC.scan.scan.index, limit)
			}
			seenGray[active] = true
		}
	} else if c.tinyGC.state == tinyIdle || c.tinyGC.state == tinySweep {
		if len(c.tinyGC.grayStack) != 0 {
			return fmt.Errorf("gc: tiny state %d retains gray-stack work", c.tinyGC.state)
		}
	}
	for h := uint32(1); int(h) < len(c.handles); h++ {
		if c.handles[h].space == spaceTiny && (c.tinyColorOf(h) == tinyGray) != seenGray[h] {
			return fmt.Errorf("gc: tiny gray handle %d is missing from queue/cursor state", h)
		}
	}
	if err := c.verifyTinyFreeList(liveBlocks); err != nil {
		return err
	}
	return nil
}

func (h *tinyHeap) verifyMetadataShape() error {
	if h.blockCount == 0 || uint64(h.blockCount)*uint64(h.blockBytes) != uint64(len(h.mem)) {
		return errors.New("gc: tiny heap geometry disagrees with memory")
	}
	if h.blockCount <= uint32(^uint16(0)) {
		if len(h.span16) != int(h.blockCount) || h.span32 != nil {
			return errors.New("gc: tiny compact boundary metadata has invalid shape")
		}
	} else if len(h.span32) != int(h.blockCount) || h.span16 != nil {
		return errors.New("gc: tiny wide boundary metadata has invalid shape")
	}
	if len(h.usedStarts) != int((uint64(h.blockCount)+63)/64) {
		return errors.New("gc: tiny allocation-start bitmap has invalid shape")
	}
	binCount := tinyBinForSize(h.blockCount) + 1
	if len(h.binHeads) != int(binCount) || len(h.binWords) != int((uint64(binCount)+63)/64) {
		return errors.New("gc: tiny free-bin metadata has invalid shape")
	}
	return nil
}

func (c *Collector) verifyTinyFreeList(live []bool) error {
	seenStarts := make([]bool, c.tiny.blockCount)
	freeBlocks := make([]bool, c.tiny.blockCount)
	var summary uint64
	for word, occupied := range c.tiny.binWords {
		if occupied != 0 {
			summary |= uint64(1) << uint32(word)
		}
	}
	if summary != c.tiny.binSummary {
		return errors.New("gc: tiny free-bin summary disagrees with occupancy")
	}
	if lastBits := uint32(len(c.tiny.binHeads)) & 63; lastBits != 0 {
		valid := (uint64(1) << lastBits) - 1
		if c.tiny.binWords[len(c.tiny.binWords)-1]&^valid != 0 {
			return errors.New("gc: tiny free-bin bitmap marks an invalid bin")
		}
	}
	for bin, head := range c.tiny.binHeads {
		marked := c.tiny.binWords[uint32(bin)>>6]&(uint64(1)<<(uint32(bin)&63)) != 0
		if (head != tinyNoBlock) != marked {
			return errors.New("gc: tiny free-bin bitmap disagrees with head")
		}
		prev := tinyNoBlock
		for b := head; b != tinyNoBlock; {
			if b >= c.tiny.blockCount || seenStarts[b] {
				return errors.New("gc: tiny free list out of bounds or cyclic")
			}
			seenStarts[b] = true
			size := c.tiny.spanSize(b)
			if size == 0 || size > c.tiny.blockCount-b || c.tiny.isUsedStart(b) || tinyBinForSize(size) != uint32(bin) {
				return errors.New("gc: malformed tiny free span")
			}
			next, recordedPrev := c.tiny.freeLinks(b)
			if recordedPrev != prev {
				return errors.New("gc: malformed tiny free links")
			}
			for i := b; i < b+size; i++ {
				if live[i] || freeBlocks[i] {
					return errors.New("gc: tiny free span overlaps another span")
				}
				freeBlocks[i] = true
			}
			prev, b = b, next
		}
	}
	var previousFree bool
	for b := uint32(0); b < c.tiny.blockCount; {
		size := c.tiny.spanSize(b)
		if size == 0 || size > c.tiny.blockCount-b || c.tiny.spanSize(b+size-1) != size {
			return errors.New("gc: malformed tiny span boundaries")
		}
		used := c.tiny.isUsedStart(b)
		if used == seenStarts[b] {
			return errors.New("gc: tiny span missing from allocation or free metadata")
		}
		if !used && previousFree {
			return errors.New("gc: adjacent tiny free spans not coalesced")
		}
		for i := b; i < b+size; i++ {
			if used != live[i] || (!used) != freeBlocks[i] {
				return errors.New("gc: tiny span coverage disagrees with handles")
			}
			if i != b && c.tiny.isUsedStart(i) {
				return errors.New("gc: tiny allocation-start bitmap marks a span interior")
			}
		}
		previousFree = !used
		b += size
	}
	return nil
}
