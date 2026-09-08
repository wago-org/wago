package gc

import (
	"cmp"
	"encoding/binary"
	"errors"
	"slices"
	"time"
)

func (c *Collector) CollectFull(roots RootSet) error {
	if c.telemetryEnabled() {
		return c.collectFullTelemetry(roots)
	}
	if err := c.errIfClosed(); err != nil {
		return err
	}
	c.discardNativeStructHandles()
	defer c.refreshNativeView()
	c.stats.FullCollections++
	if c.cfg.Profile == ProfileTiny {
		if err := c.tinyCollectFull(roots); err != nil {
			return err
		}
		if c.cfg.VerifyAfterCollect {
			return c.Verify(roots)
		}
		return nil
	}
	c.clearMarks()
	c.markRoots(roots)
	c.sweepAll()
	c.pruneRemembered()
	c.finishFullCardMetadata()
	if c.cfg.VerifyAfterCollect {
		return c.Verify(roots)
	}
	return nil
}

func (c *Collector) collectFullTelemetry(roots RootSet) (err error) {
	if err = c.errIfClosed(); err != nil {
		return err
	}
	c.discardNativeStructHandles()
	defer c.refreshNativeView()
	c.stats.FullCollections++
	c.beginCollectionTelemetry(telemetryFull)
	success := false
	defer func() { c.endCollectionTelemetry(success) }()
	if c.cfg.Profile == ProfileTiny {
		if err = c.tinyCollectFull(roots); err != nil {
			return err
		}
		if c.cfg.VerifyAfterCollect {
			c.cfg.Telemetry.suspend()
			err = c.Verify(roots)
			c.cfg.Telemetry.resume()
			if err != nil {
				return err
			}
		}
		success = true
		return nil
	}
	c.cfg.Telemetry.setPhase(telemetryPhaseMetadataCleanup)
	c.clearMarks()
	c.cfg.Telemetry.setPhase(telemetryPhaseRootEnumeration)
	c.enumerateRoots(roots, rootMarkFull)
	c.cfg.Telemetry.setPhase(telemetryPhaseMarking)
	c.drainMarkStack()
	c.cfg.Telemetry.setPhase(telemetryPhaseSweep)
	c.sweepAllTelemetry()
	c.cfg.Telemetry.setPhase(telemetryPhaseMetadataCleanup)
	c.pruneRemembered()
	c.finishFullCardMetadata()
	if c.cfg.VerifyAfterCollect {
		c.cfg.Telemetry.suspend()
		err = c.Verify(roots)
		c.cfg.Telemetry.resume()
		if err != nil {
			return err
		}
	}
	success = true
	return nil
}

func (c *Collector) CollectMinor(roots RootSet) error {
	if c.telemetryEnabled() {
		return c.collectMinorTelemetry(roots)
	}
	if err := c.errIfClosed(); err != nil {
		return err
	}
	c.discardNativeStructHandles()
	defer c.refreshNativeView()
	c.stats.MinorCollections++
	if c.cfg.Profile == ProfileTiny {
		if err := c.tinyCollectFull(roots); err != nil {
			return err
		}
		if c.cfg.VerifyAfterCollect {
			return c.Verify(roots)
		}
		return nil
	}
	policyStart := time.Time{}
	if c.cfg.MinorPauseTargetMicros != 0 {
		policyStart = time.Now()
	}
	c.clearNurseryMarks()
	c.markNurseryRoots(roots)
	if c.cfg.VerifyAfterCollect {
		if err := c.verifyRememberedShadow(); err != nil {
			return err
		}
	}
	for _, h := range c.remembered {
		if int(h) < len(c.handles) && !c.handles[h].young() && (c.handles[h].space == spaceOld || c.handles[h].space == spaceLarge) {
			c.stats.MinorRememberedScanned++
			c.scanRememberedCards(h)
		}
	}
	var copiedBytes, promotedBytes uint64
	if survivors := c.drainNurseryMarkStack(); survivors != 0 {
		var err error
		copiedBytes, promotedBytes, err = c.promoteMarkedNursery()
		if err != nil {
			c.clearNurseryMarks()
			return err
		}
	}
	c.finishMinorEvacuation()
	c.finishMinorCardMetadata()
	pauseNS := uint64(0)
	if !policyStart.IsZero() {
		pauseNS = uint64(time.Since(policyStart))
	}
	c.adaptTenuring(copiedBytes, promotedBytes, pauseNS)
	if c.cfg.VerifyAfterCollect {
		if err := c.verifyNurseryEvacuated(); err != nil {
			return err
		}
	}
	if c.cfg.ForceMajorEveryMinor {
		if err := c.CollectFull(roots); err != nil {
			return err
		}
	}
	if c.cfg.VerifyAfterCollect {
		return c.Verify(roots)
	}
	return nil
}

func (c *Collector) collectMinorTelemetry(roots RootSet) (err error) {
	if err = c.errIfClosed(); err != nil {
		return err
	}
	c.discardNativeStructHandles()
	defer c.refreshNativeView()
	c.stats.MinorCollections++
	c.beginCollectionTelemetry(telemetryMinor)
	success, ended := false, false
	defer func() {
		if !ended {
			c.endCollectionTelemetry(success)
		}
	}()
	if c.cfg.Profile == ProfileTiny {
		// Tiny is non-generational; minor collection is defined as a complete
		// incremental mark/sweep cycle for API compatibility.
		if err = c.tinyCollectFull(roots); err != nil {
			return err
		}
		if c.cfg.VerifyAfterCollect {
			c.cfg.Telemetry.suspend()
			err = c.Verify(roots)
			c.cfg.Telemetry.resume()
			if err != nil {
				return err
			}
		}
		success = true
		return nil
	}
	policyStart := time.Time{}
	if c.cfg.MinorPauseTargetMicros != 0 {
		policyStart = time.Now()
	}
	// Minor collection traces nursery reachability only. Exact transient roots,
	// dirty persistent slots, and dirty old/large payload cards are the complete
	// inputs; clean persistent slots and clean old-object cards are not scanned.
	c.cfg.Telemetry.setPhase(telemetryPhaseMetadataCleanup)
	c.clearNurseryMarks()
	c.cfg.Telemetry.setPhase(telemetryPhaseRootEnumeration)
	c.markNurseryRoots(roots)
	if c.cfg.VerifyAfterCollect {
		c.cfg.Telemetry.suspend()
		err = c.verifyRememberedShadow()
		c.cfg.Telemetry.resume()
		if err != nil {
			return err
		}
	}
	c.cfg.Telemetry.setPhase(telemetryPhaseRememberedRoots)
	if c.telemetryEnabled() {
		c.cfg.Telemetry.active.rememberedScan = true
	}
	for _, h := range c.remembered {
		if int(h) < len(c.handles) && !c.handles[h].young() && (c.handles[h].space == spaceOld || c.handles[h].space == spaceLarge) {
			c.stats.MinorRememberedScanned++
			c.scanRememberedCards(h)
		}
	}
	if c.telemetryEnabled() {
		c.cfg.Telemetry.active.rememberedScan = false
	}
	c.cfg.Telemetry.setPhase(telemetryPhaseTracing)
	var copiedBytes, promotedBytes uint64
	if survivors := c.drainNurseryMarkStack(); survivors != 0 {
		c.cfg.Telemetry.setPhase(telemetryPhasePromotionCopy)
		copiedBytes, promotedBytes, err = c.promoteMarkedNursery()
		if err != nil {
			c.clearNurseryMarks()
			return err
		}
	}
	c.cfg.Telemetry.setPhase(telemetryPhaseSweep)
	c.finishMinorEvacuationTelemetry()
	c.cfg.Telemetry.setPhase(telemetryPhaseMetadataCleanup)
	c.finishMinorCardMetadata()
	pauseNS := uint64(0)
	if !policyStart.IsZero() {
		pauseNS = uint64(time.Since(policyStart))
	}
	c.adaptTenuring(copiedBytes, promotedBytes, pauseNS)
	if c.cfg.VerifyAfterCollect {
		c.cfg.Telemetry.suspend()
		err = c.verifyNurseryEvacuated()
		c.cfg.Telemetry.resume()
		if err != nil {
			return err
		}
		c.cfg.Telemetry.suspend()
		err = c.Verify(roots)
		c.cfg.Telemetry.resume()
		if err != nil {
			return err
		}
	}
	success = true
	c.endCollectionTelemetry(true)
	ended = true
	if c.cfg.ForceMajorEveryMinor {
		if err = c.CollectFull(roots); err != nil {
			return err
		}
	}
	return nil
}
func (c *Collector) finishFullCardMetadata() {
	// Full Throughput collection does not evacuate live nursery objects. Retain
	// cards while any nursery allocation survives so the next minor collection
	// still has complete old-edge and persistent-root inputs.
	if len(c.nurseryHandles) == 0 {
		c.clearCardMetadata()
	}
}

func (c *Collector) sweepAll() {
	for h := uint32(1); int(h) < len(c.handles); h++ {
		if c.handles[h].space != spaceFree && !c.mark[h] {
			if c.handles[h].space == spaceOld || c.handles[h].space == spaceLarge {
				c.deferThroughputFree(h)
			} else {
				c.free(h)
			}
		}
	}
	c.compactNurseryHandles()
}

func (c *Collector) sweepAllTelemetry() {
	for h := uint32(1); int(h) < len(c.handles); h++ {
		if c.handles[h].space != spaceFree && !c.mark[h] {
			c.cfg.Telemetry.noteSweep(c.handles[h].size)
			if c.handles[h].space == spaceOld || c.handles[h].space == spaceLarge {
				c.deferThroughputFree(h)
			} else {
				c.free(h)
			}
		}
	}
	c.compactNurseryHandles()
}

// finishMinorEvacuation reclaims dead young handles and retains only live
// survivor/large-young handles. promoteMarkedNursery has already copied or
// tenured every marked object transactionally.
func (c *Collector) finishMinorEvacuation() { c.finishMinorEvacuationMode(false) }

func (c *Collector) finishMinorEvacuationTelemetry() { c.finishMinorEvacuationMode(true) }

func (c *Collector) finishMinorEvacuationMode(measured bool) {
	out := c.nurseryHandles[:0]
	for _, h := range c.nurseryHandles {
		if h == 0 || int(h) >= len(c.handles) {
			continue
		}
		live := c.mark[h]
		c.mark[h] = false
		if live {
			if c.handles[h].young() {
				out = append(out, h)
			}
			continue
		}
		if c.handles[h].young() {
			if measured {
				c.cfg.Telemetry.noteSweep(c.handles[h].size)
			}
			c.free(h)
		}
	}
	clear(c.nurseryHandles[len(out):])
	c.nurseryHandles = out
	c.nurseryBump = 0
	if len(out) == 0 {
		c.survivorBump = 0
		c.survivorFrom ^= 1
	}
}

type plannedPromotion struct {
	handle uint32
	entry  handleEntry
}

// promoteMarkedNursery plans survivor-space copies, old-space promotions, and
// in-place large-object aging before publishing any handle location. Inactive
// survivor bytes are written only after destination and commit failure points
// have all passed, so rollback leaves the complete observable heap unchanged.
func (c *Collector) promoteMarkedNursery() (copiedBytes, promotedBytes uint64, err error) {
	if c.survivorBytes == 0 && c.tenuringThreshold == 1 {
		return c.promoteMarkedNurseryImmediate()
	}
	plans := c.promotionScratch[:0]
	needsOldAllocation := false
	toSpace := c.survivorFrom ^ 1
	toBase := c.survivorBase(toSpace)
	toBump := uint32(0)
	finish := func() {
		clear(plans)
		c.promotionScratch = plans[:0]
	}
	for _, h := range c.nurseryHandles {
		if !c.isYoungHandle(h) || !c.mark[h] {
			continue
		}
		if err = injectFailure(c, failPromotionPlan); err != nil {
			finish()
			return 0, 0, err
		}
		src := c.handles[h]
		age := src.age() + 1
		plan := plannedPromotion{handle: h}
		switch {
		case src.space == spaceLarge:
			plan.entry = src
			if age >= c.tenuringThreshold {
				plan.entry.clearYoungAge()
			} else {
				plan.entry.setYoungAge(age)
			}
		case age < c.tenuringThreshold && c.survivorBytes != 0:
			off := align(toBump, max(srcAlignment(c, h), uint32(8)))
			if off <= c.survivorBytes && src.size <= c.survivorBytes-off {
				plan.entry = handleEntry{off: toBase + off, size: src.size, allocSize: src.size, space: spaceNursery}
				plan.entry.setYoungAge(age)
				toBump = off + src.size
			}
		}
		plans = append(plans, plan)
		needsOldAllocation = needsOldAllocation || plan.entry.space == spaceFree
	}
	if needsOldAllocation && len(plans) > 1 && !c.promotionPlansSorted(plans) {
		slices.SortStableFunc(plans, func(a, b plannedPromotion) int {
			ak, bk := promotionPlanKind(a.entry), promotionPlanKind(b.entry)
			if ak != bk {
				return cmp.Compare(ak, bk)
			}
			if ak == 1 {
				return cmp.Compare(c.throughput.promotionAllocSize(c.handles[a.handle].size), c.throughput.promotionAllocSize(c.handles[b.handle].size))
			}
			return 0
		})
	}
	if !needsOldAllocation {
		// A pure survivor/large-young cycle cannot mutate old-space allocation
		// state. Preserve every debug failure point, but keep AVL synchronization
		// and allocation rollback machinery out of this common minor path.
		for range plans {
			if err = injectFailure(c, failPromotionDestination); err != nil {
				finish()
				return 0, 0, err
			}
		}
		for range plans {
			if err = injectFailure(c, failPromotionCommit); err != nil {
				finish()
				return 0, 0, err
			}
		}
		return c.commitPromotionPlans(plans, toSpace, toBump)
	}
	if err = c.throughput.sweepAllPending(); err != nil {
		finish()
		return 0, 0, err
	}
	tx := c.throughput.beginAllocTransaction()
	rollback := func(current *handleEntry) {
		if current != nil {
			c.throughput.rollbackSuccessfulAlloc(*current, tx.bump)
		}
		for i := len(plans) - 1; i >= 0; i-- {
			if plans[i].entry.space == spaceOld {
				c.throughput.rollbackSuccessfulAlloc(plans[i].entry, tx.bump)
			}
		}
		c.throughput.restoreAllocTransaction(tx)
		finish()
	}
	for i := 0; i < len(plans); {
		if plans[i].entry.space != spaceFree {
			if err = injectFailure(c, failPromotionDestination); err != nil {
				rollback(nil)
				return 0, 0, err
			}
			i++
			continue
		}
		size := c.handles[plans[i].handle].size
		j := i + 1
		for j < len(plans) && plans[j].entry.space == spaceFree && c.handles[plans[j].handle].size == size {
			j++
		}
		if run, ok, runErr := c.throughput.tryAllocRun(size, j-i, spaceOld); runErr != nil {
			rollback(nil)
			return 0, 0, runErr
		} else if ok {
			for k := i; k < j; k++ {
				allocSize, class := run.allocSize, run.class
				if k == j-1 && run.lastAllocSize != run.allocSize {
					allocSize = run.lastAllocSize
					class = uint16(len(throughputClassSizes))
				}
				plans[k].entry = handleEntry{off: run.off + uint32(k-i)*run.allocSize, size: size, allocSize: allocSize, class: class, space: spaceOld}
			}
			for k := i; k < j; k++ {
				if err = injectFailure(c, failPromotionDestination); err != nil {
					rollback(nil)
					return 0, 0, err
				}
			}
			i = j
			continue
		}
		e, allocErr := c.allocThroughput(size, spaceOld)
		if allocErr != nil {
			rollback(nil)
			return 0, 0, allocErr
		}
		if err = injectFailure(c, failPromotionDestination); err != nil {
			rollback(&e)
			return 0, 0, err
		}
		plans[i].entry = e
		i++
	}
	for range plans {
		if err = injectFailure(c, failPromotionCommit); err != nil {
			rollback(nil)
			return 0, 0, err
		}
	}
	return c.commitPromotionPlans(plans, toSpace, toBump)
}

func (c *Collector) commitPromotionPlans(plans []plannedPromotion, toSpace uint8, toBump uint32) (copiedBytes, promotedBytes uint64, err error) {
	hasYoung, hasTenured := false, false
	for _, p := range plans {
		src := c.handles[p.handle]
		size := src.size
		age := src.age() + 1
		switch p.entry.space {
		case spaceNursery:
			dst := c.nursery[p.entry.off : p.entry.off+p.entry.size]
			copy(dst, c.bytes(makeObjRef(p.handle)))
			c.poisonYoungSource(src)
			c.handles[p.handle] = p.entry
			copiedBytes += uint64(size)
		case spaceOld:
			c.promoteHandleTo(p.handle, p.entry)
			copiedBytes += uint64(size)
			promotedBytes += uint64(size)
		case spaceLarge:
			if p.entry.young() {
				c.ageLargeInPlace(p.handle, p.entry.age())
			} else {
				c.tenureLargeInPlace(p.handle)
				promotedBytes += uint64(size)
			}
		}
		if c.handles[p.handle].young() {
			hasYoung = true
		} else {
			hasTenured = true
		}
		if c.telemetryEnabled() {
			pointerFree := c.header(makeObjRef(p.handle)).Flags&FlagPointerFree != 0
			c.cfg.Telemetry.noteSurvivor(size, age, pointerFree, p.entry.space == spaceNursery)
			if !p.entry.young() {
				c.cfg.Telemetry.notePromotion(size, p.entry.space == spaceOld)
			}
		}
	}
	// A parent can be older than a child. Once all handle locations and ages are
	// published, establish cards for newly tenured parents that still point into
	// either survivor space or a young large object. Homogeneous-age cycles have
	// no possible old-to-young edge and skip descriptor/payload inspection.
	if hasYoung && hasTenured {
		for _, p := range plans {
			if !c.handles[p.handle].young() && c.handleContainsNurseryRef(p.handle) {
				c.remember(p.handle)
				c.markWholeObjectCard(p.handle)
			}
		}
	}
	c.survivorFrom = toSpace
	c.survivorBump = toBump
	c.stats.YoungBytesCopied += copiedBytes
	c.stats.PromotedBytes += promotedBytes
	clear(plans)
	c.promotionScratch = plans[:0]
	return copiedBytes, promotedBytes, nil
}

func (c *Collector) promoteMarkedNurseryImmediate() (copiedBytes, promotedBytes uint64, err error) {
	plans := c.promotionScratch[:0]
	finish := func() {
		clear(plans)
		c.promotionScratch = plans[:0]
	}
	plansSorted := true
	previousKind := -1
	var previousSize uint32
	for _, h := range c.nurseryHandles {
		if !c.isYoungHandle(h) || !c.mark[h] {
			continue
		}
		if err = injectFailure(c, failPromotionPlan); err != nil {
			finish()
			return 0, 0, err
		}
		plan := plannedPromotion{handle: h}
		if c.handles[h].space == spaceLarge {
			plan.entry = c.handles[h]
			plan.entry.clearYoungAge()
		}
		kind, size := promotionPlanKind(plan.entry), c.handles[h].size
		if previousKind >= 0 && (kind < previousKind || (kind == 1 && previousKind == 1 && size < previousSize)) {
			plansSorted = false
		}
		previousKind, previousSize = kind, size
		plans = append(plans, plan)
	}
	if len(plans) > 1 && !plansSorted {
		slices.SortStableFunc(plans, func(a, b plannedPromotion) int {
			ak, bk := promotionPlanKind(a.entry), promotionPlanKind(b.entry)
			if ak != bk {
				return cmp.Compare(ak, bk)
			}
			if ak == 1 {
				return cmp.Compare(c.throughput.promotionAllocSize(c.handles[a.handle].size), c.throughput.promotionAllocSize(c.handles[b.handle].size))
			}
			return 0
		})
	}
	if err = c.throughput.sweepAllPending(); err != nil {
		finish()
		return 0, 0, err
	}
	tx := c.throughput.beginAllocTransaction()
	rollback := func(current *handleEntry) {
		if current != nil {
			c.throughput.rollbackSuccessfulAlloc(*current, tx.bump)
		}
		for i := len(plans) - 1; i >= 0; i-- {
			if plans[i].entry.space == spaceOld {
				c.throughput.rollbackSuccessfulAlloc(plans[i].entry, tx.bump)
			}
		}
		c.throughput.restoreAllocTransaction(tx)
		finish()
	}
	for i := 0; i < len(plans); {
		if plans[i].entry.space != spaceFree {
			if err = injectFailure(c, failPromotionDestination); err != nil {
				rollback(nil)
				return 0, 0, err
			}
			i++
			continue
		}
		size := c.handles[plans[i].handle].size
		j := i + 1
		for j < len(plans) && plans[j].entry.space == spaceFree && c.handles[plans[j].handle].size == size {
			j++
		}
		if run, ok, runErr := c.throughput.tryAllocRun(size, j-i, spaceOld); runErr != nil {
			rollback(nil)
			return 0, 0, runErr
		} else if ok {
			for k := i; k < j; k++ {
				allocSize, class := run.allocSize, run.class
				if k == j-1 && run.lastAllocSize != run.allocSize {
					allocSize = run.lastAllocSize
					class = uint16(len(throughputClassSizes))
				}
				plans[k].entry = handleEntry{off: run.off + uint32(k-i)*run.allocSize, size: size, allocSize: allocSize, class: class, space: spaceOld}
			}
			for k := i; k < j; k++ {
				if err = injectFailure(c, failPromotionDestination); err != nil {
					rollback(nil)
					return 0, 0, err
				}
			}
			i = j
			continue
		}
		e, allocErr := c.allocThroughput(size, spaceOld)
		if allocErr != nil {
			rollback(nil)
			return 0, 0, allocErr
		}
		if err = injectFailure(c, failPromotionDestination); err != nil {
			rollback(&e)
			return 0, 0, err
		}
		plans[i].entry = e
		i++
	}
	for range plans {
		if err = injectFailure(c, failPromotionCommit); err != nil {
			rollback(nil)
			return 0, 0, err
		}
	}
	for _, p := range plans {
		size := c.handles[p.handle].size
		if p.entry.space == spaceLarge {
			c.tenureLargeInPlace(p.handle)
			promotedBytes += uint64(size)
		} else {
			c.promoteHandleTo(p.handle, p.entry)
			copiedBytes += uint64(size)
			promotedBytes += uint64(size)
		}
		if c.telemetryEnabled() {
			pointerFree := c.header(makeObjRef(p.handle)).Flags&FlagPointerFree != 0
			c.cfg.Telemetry.noteSurvivor(size, 1, pointerFree, false)
			c.cfg.Telemetry.notePromotion(size, p.entry.space == spaceOld)
		}
	}
	c.survivorFrom ^= 1
	c.survivorBump = 0
	c.stats.YoungBytesCopied += copiedBytes
	c.stats.PromotedBytes += promotedBytes
	finish()
	return copiedBytes, promotedBytes, nil
}

func (c *Collector) promotionPlansSorted(plans []plannedPromotion) bool {
	previousKind := promotionPlanKind(plans[0].entry)
	previousSize := c.handles[plans[0].handle].size
	for _, plan := range plans[1:] {
		kind := promotionPlanKind(plan.entry)
		size := c.handles[plan.handle].size
		if kind < previousKind || (kind == 1 && previousKind == 1 && size < previousSize) {
			return false
		}
		previousKind, previousSize = kind, size
	}
	return true
}

func promotionPlanKind(e handleEntry) int {
	switch e.space {
	case spaceNursery:
		return 0
	case spaceFree:
		return 1
	default:
		return 2
	}
}

func srcAlignment(c *Collector, h uint32) uint32 {
	// Promotion planning already validated h as a marked young handle. Read the
	// immutable canonical descriptor directly instead of repeating the public
	// ref/closed/handle checks for every survivor.
	typeID := TypeID(binary.LittleEndian.Uint32(c.bytes(makeObjRef(h))))
	if int(typeID) < len(c.types) && c.types[typeID].Align > 8 {
		return c.types[typeID].Align
	}
	return 8
}

func (c *Collector) promoteHandle(h uint32) error {
	if int(h) >= len(c.handles) || c.handles[h].space == spaceFree {
		return errors.New("gc: invalid handle")
	}
	if !c.handles[h].young() {
		return nil
	}
	if c.handles[h].space == spaceLarge {
		c.tenureLargeInPlace(h)
		c.removeYoungHandle(h)
		return nil
	}
	if err := c.throughput.sweepAllPending(); err != nil {
		return err
	}
	tx := c.throughput.beginAllocTransaction()
	oldEntry, err := c.allocThroughput(c.handles[h].size, spaceOld)
	if err != nil {
		c.throughput.restoreAllocTransaction(tx)
		return err
	}
	c.promoteHandleTo(h, oldEntry)
	c.removeYoungHandle(h)
	return nil
}

func (c *Collector) promoteHandleTo(h uint32, oldEntry handleEntry) {
	e := &c.handles[h]
	src := c.nursery[e.off : e.off+e.size]
	dst := c.throughput.bytes(oldEntry)
	copy(dst, src)
	flags := binary.LittleEndian.Uint32(dst[12:16]) | FlagOld
	binary.LittleEndian.PutUint32(dst[12:16], flags)
	if c.cfg.PoisonFreed {
		for i := range src {
			src[i] = 0xdd
		}
	}
	*e = oldEntry
}
func (c *Collector) free(h uint32) { c.releaseHandle(h, false, true) }

func (c *Collector) freeTinyPrepoisoned(h uint32) { c.releaseHandle(h, false, false) }

func (c *Collector) deferThroughputFree(h uint32) { c.releaseHandle(h, true, true) }

func (c *Collector) releaseHandle(h uint32, lazyThroughput, poisonTiny bool) {
	e := &c.handles[h]
	// A live handle is the allocator ownership proof. Reject internal metadata
	// corruption before clearing the handle or its remembered/card state; losing
	// either would make the allocation unreachable and unrecoverable. These errors
	// cannot be caused by guest input after allocation has succeeded, so fail-stop
	// is safer than continuing with a corrupted allocator.
	switch e.space {
	case spaceTiny:
		var err error
		if poisonTiny {
			err = c.tiny.free(e.off)
		} else {
			err = c.tiny.freeWithoutPoison(e.off)
		}
		if err != nil {
			panic("gc: internal tiny free invariant: " + err.Error())
		}
		c.tinySetWhite(h)
	case spaceOld, spaceLarge:
		var err error
		if lazyThroughput {
			err = c.throughput.deferFree(*e)
		} else {
			err = c.throughput.free(*e)
		}
		if err != nil {
			panic("gc: internal throughput free invariant: " + err.Error())
		}
	}
	c.removeRemembered(h)
	c.removeCardsForHandle(h)
	if c.cfg.PoisonFreed {
		switch e.space {
		case spaceNursery:
			for i := range c.nursery[e.off : e.off+e.size] {
				c.nursery[e.off+uint32(i)] = 0xdd
			}
		case spaceOld, spaceLarge:
			end := e.off + e.allocSize
			if end > uint32(len(c.throughput.mem)) {
				end = uint32(len(c.throughput.mem))
			}
			for i := range c.throughput.mem[e.off:end] {
				c.throughput.mem[e.off+uint32(i)] = 0xdd
			}
		}
	}
	*e = handleEntry{}
	c.freeHandles = append(c.freeHandles, h)
}
func (c *Collector) compactNurseryHandles() {
	out := c.nurseryHandles[:0]
	var edenMax uint32
	if c.survivorBump == 0 {
		// Native array chunks and direct native structs share one reserved handle
		// batch. Their handle publication order can differ from their physical
		// Eden order, so derive the bump from every live extent instead of trusting
		// the last retained handle.
		for _, h := range c.nurseryHandles {
			if h == 0 || int(h) >= len(c.handles) {
				continue
			}
			e := c.handles[h]
			sp := e.space
			if sp == spaceNursery || (sp == spaceLarge && c.handles[h].young()) {
				out = append(out, h)
			}
			if sp == spaceNursery && e.off < c.edenBytes() && e.off+e.size > edenMax {
				edenMax = e.off + e.size
			}
		}
		clear(c.nurseryHandles[len(out):])
		c.nurseryHandles = out
		c.nurseryBump = edenMax
		return
	}
	var survivorMax uint32
	base := c.survivorBase(c.survivorFrom)
	for _, h := range c.nurseryHandles {
		if !c.isYoungHandle(h) {
			continue
		}
		out = append(out, h)
		e := c.handles[h]
		if e.space != spaceNursery {
			continue
		}
		end := e.off + e.size
		if e.off < c.edenBytes() {
			if end > edenMax {
				edenMax = end
			}
		} else if e.off >= base && end <= base+c.survivorBytes && end-base > survivorMax {
			survivorMax = end - base
		}
	}
	clear(c.nurseryHandles[len(out):])
	c.nurseryHandles = out
	c.nurseryBump = edenMax
	c.survivorBump = survivorMax
}
func (c *Collector) liveCount() uint32 {
	var n uint32
	for _, e := range c.handles {
		if e.space != spaceFree {
			n++
		}
	}
	return n
}
