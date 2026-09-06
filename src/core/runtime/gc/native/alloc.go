package gc

import (
	"encoding/binary"
	"errors"
	"fmt"
)

func (c *Collector) alloc(d TypeDesc, size, aux uint32, roots RootSet) (Ref, error) {
	if c.nativeStructAlloc.Count != 0 || c.nativeStructAlloc.ChunkEnd != 0 {
		c.discardNativeStructHandles()
	}
	if c.cfg.Profile == ProfileTiny {
		return c.tinyAlloc(d, size, aux, roots)
	}
	if err := c.errIfClosed(); err != nil {
		return Null(), err
	}
	if c.cfg.DisableCollection {
		var tx throughputAllocTransaction
		if failureInjectionEnabled {
			if err := c.throughput.sweepAllPending(); err != nil {
				return Null(), err
			}
			tx = c.throughput.beginAllocTransaction()
		}
		e, err := c.allocThroughput(size, spaceLarge)
		if err != nil {
			if isInjectedFailure(err) {
				c.throughput.restoreAllocTransaction(tx)
				return Null(), err
			}
			return Null(), fmt.Errorf("gc: collection-disabled heap exhausted: %w", err)
		}
		if err := injectFailure(c, failHandlePublication); err != nil {
			c.throughput.rollbackSuccessfulAlloc(e, tx.bump)
			c.throughput.restoreAllocTransaction(tx)
			return Null(), err
		}
		h := c.newHandle(e)
		r := makeObjRef(h)
		flags := uint32(FlagLarge)
		if !d.HasRefs {
			flags |= FlagPointerFree
		}
		c.writeHeader(r, ObjHeader{TypeID: uint32(d.ID), Size: size, Aux: aux, Flags: flags})
		c.stats.Allocations++
		if c.telemetryEnabled() {
			c.cfg.Telemetry.paths.GoAllocationPaths++
		}
		c.refreshNativeView()
		return r, nil
	}
	if c.cfg.CollectEveryAlloc {
		if roots == nil {
			return Null(), errors.New("gc: allocation-triggered collection requires roots")
		}
		var err error
		if stressFullCollection() {
			err = c.CollectFull(roots)
		} else {
			err = c.CollectMinor(roots)
		}
		if err != nil {
			if err := c.retryAllocationMinorAfterPromotionFailure(roots, err); err != nil {
				return Null(), err
			}
		}
	}
	sp := spaceNursery
	var off uint32
	var e handleEntry
	var oldNurseryBump uint32
	var allocationTx throughputAllocTransaction
	if c.shouldAllocateLarge(size) {
		var err error
		if failureInjectionEnabled {
			if err := c.throughput.sweepAllPending(); err != nil {
				return Null(), err
			}
			allocationTx = c.throughput.beginAllocTransaction()
		}
		e, err = c.allocThroughput(size, spaceLarge)
		if err != nil {
			if isInjectedFailure(err) {
				c.throughput.restoreAllocTransaction(allocationTx)
				return Null(), err
			}
			if errors.Is(err, ErrAllocationTooLarge) {
				return Null(), err
			}
			if roots == nil {
				return Null(), errors.New("gc: large-object space exhausted and no roots were supplied")
			}
			if err := c.CollectFull(roots); err != nil {
				return Null(), err
			}
			if failureInjectionEnabled {
				if err := c.throughput.sweepAllPending(); err != nil {
					return Null(), err
				}
				allocationTx = c.throughput.beginAllocTransaction()
			}
			e, err = c.allocThroughput(size, spaceLarge)
			if err != nil {
				return Null(), err
			}
		}
		sp = spaceLarge
		off = e.off
		if !c.cfg.DisableMovingNursery {
			e.setYoungAge(0)
		}
	} else {
		objectAlign := d.Align
		if objectAlign < 8 {
			objectAlign = 8
		}
		nurseryOffset := func() (uint32, bool) {
			off := align(c.nurseryBump, objectAlign)
			limit := c.edenBytes()
			return off, off <= limit && size <= limit-off
		}
		var fits bool
		off, fits = nurseryOffset()
		if !fits {
			if c.telemetryEnabled() {
				c.cfg.Telemetry.paths.NurseryExhaustions++
			}
			if roots == nil {
				return Null(), errors.New("gc: nursery exhausted and no roots were supplied")
			}
			if err := c.CollectMinor(roots); err != nil {
				if err := c.retryAllocationMinorAfterPromotionFailure(roots, err); err != nil {
					return Null(), err
				}
			}
			off, fits = nurseryOffset()
			if !fits {
				return Null(), errors.New("gc: nursery exhausted without roots")
			}
		}
		oldNurseryBump = c.nurseryBump
		c.nurseryBump = off + size
	}
	if sp == spaceNursery {
		e = handleEntry{off: off, size: size, allocSize: size, space: sp}
	}
	if err := injectFailure(c, failHandlePublication); err != nil {
		if sp == spaceNursery {
			c.nurseryBump = oldNurseryBump
		} else {
			c.throughput.rollbackSuccessfulAlloc(e, allocationTx.bump)
			c.throughput.restoreAllocTransaction(allocationTx)
		}
		return Null(), err
	}
	h := c.newHandle(e)
	if sp == spaceNursery || (sp == spaceLarge && e.young()) {
		c.nurseryHandles = append(c.nurseryHandles, h)
	}
	r := makeObjRef(h)
	flags := uint32(0)
	if !d.HasRefs {
		flags |= FlagPointerFree
	}
	if sp == spaceLarge {
		flags |= FlagLarge
	}
	c.writeHeader(r, ObjHeader{TypeID: uint32(d.ID), Size: size, Aux: aux, Flags: flags})
	c.stats.Allocations++
	if c.telemetryEnabled() {
		c.cfg.Telemetry.paths.GoAllocationPaths++
	}
	if sp == spaceNursery {
		c.refreshNativeHandles()
	} else {
		c.refreshNativeView()
	}
	return r, nil
}

//go:noinline
func (c *Collector) retryAllocationMinorAfterPromotionFailure(roots RootSet, promotionErr error) error {
	if !errors.Is(promotionErr, errThroughputHeapExhausted) {
		return promotionErr
	}
	// Promotion planning is transactional: on exhaustion no nursery object has
	// moved. Reclaim unreachable old/large objects with the exact allocation
	// roots, then rerun the minor collection to promote the same survivors.
	if err := c.CollectFull(roots); err != nil {
		return err
	}
	return c.CollectMinor(roots)
}

func (c *Collector) shouldAllocateLarge(size uint32) bool {
	// The large-object threshold is a policy preference, but nursery capacity is
	// a hard safety boundary: an object that cannot fit in an empty nursery must
	// be allocated in non-moving large space even when tests choose a higher
	// threshold to stress tiny nurseries.
	return size >= c.cfg.LargeObjectBytes || size > c.edenBytes()
}

func (c *Collector) newHandle(e handleEntry) uint32 {
	for len(c.freeHandles) > 0 {
		n := len(c.freeHandles)
		h := c.freeHandles[n-1]
		c.freeHandles = c.freeHandles[:n-1]
		if c.checkedHandles != nil {
			generations := *c.checkedHandles
			if generations[h] == ^uint64(0) {
				continue
			} // retire rather than wrap
			generations[h]++
		}
		c.handles[h] = e
		return h
	}
	c.handles = append(c.handles, e)
	if c.checkedHandles != nil {
		*c.checkedHandles = append(*c.checkedHandles, 1)
	}
	c.mark = append(c.mark, false)
	// The handle is unpublished until the profile-specific post-allocation path.
	// Tiny overwrites this byte with its current-epoch gray/black state; keeping
	// the shared Throughput handle-growth path at a zero append avoids irrelevant
	// epoch encoding work.
	c.tinyGC.color = append(c.tinyGC.color, 0)
	return uint32(len(c.handles) - 1)
}

func (c *Collector) desc(id TypeID) (TypeDesc, error) {
	if int(id) >= len(c.types) {
		return TypeDesc{}, fmt.Errorf("gc: unknown type id %d", id)
	}
	return c.types[id], nil
}
func (c *Collector) refDesc(r Ref) (TypeDesc, error) {
	if err := c.errIfClosed(); err != nil {
		return TypeDesc{}, err
	}
	if !r.IsObj() {
		return TypeDesc{}, errors.New("gc: ref is not object")
	}
	h := handleOf(r)
	if h == 0 || int(h) >= len(c.handles) || c.handles[h].space == spaceFree {
		return TypeDesc{}, errors.New("gc: invalid object ref")
	}
	return c.desc(TypeID(c.header(r).TypeID))
}
func (c *Collector) validObjectRef(r Ref) bool {
	if !r.IsObj() {
		return false
	}
	h := handleOf(r)
	return h != 0 && int(h) < len(c.handles) && c.handles[h].space != spaceFree
}

func (c *Collector) validateStoredRef(r Ref, nullable bool) error {
	if r.IsNull() {
		if nullable {
			return nil
		}
		return errors.New("gc: cannot store null in non-null ref slot")
	}
	if r.IsI31() {
		return nil
	}
	if !c.validObjectRef(r) {
		return errors.New("gc: invalid object ref")
	}
	if c.cfg.Profile == ProfileTiny && c.tinySweepActive() {
		if c.tinyGC.scan.handle == handleOf(r) {
			return errors.New("gc: Tiny object is pending bounded sweep reclamation")
		}
		if err := c.validateTinySweepRootPublication(r); err != nil {
			return err
		}
	}
	return nil
}

func (c *Collector) entry(r Ref) *handleEntry { return &c.handles[handleOf(r)] }
func (c *Collector) bytes(r Ref) []byte {
	e := c.entry(r)
	switch e.space {
	case spaceNursery:
		return c.nursery[e.off : e.off+e.size]
	case spaceOld, spaceLarge:
		return c.throughput.bytes(*e)
	case spaceTiny:
		return c.tiny.bytes(e.off, e.size)
	default:
		return nil
	}
}
func (c *Collector) header(r Ref) ObjHeader {
	b := c.bytes(r)
	return ObjHeader{binary.LittleEndian.Uint32(b[0:4]), binary.LittleEndian.Uint32(b[4:8]), binary.LittleEndian.Uint32(b[8:12]), binary.LittleEndian.Uint32(b[12:16])}
}
func (c *Collector) writeHeader(r Ref, h ObjHeader) {
	b := c.bytes(r)
	binary.LittleEndian.PutUint32(b[0:4], h.TypeID)
	binary.LittleEndian.PutUint32(b[4:8], h.Size)
	binary.LittleEndian.PutUint32(b[8:12], h.Aux)
	binary.LittleEndian.PutUint32(b[12:16], h.Flags)
}

func (c *Collector) zeroObjectPayload(r Ref) {
	if !c.validObjectRef(r) {
		return
	}
	e := c.entry(r)
	end := e.off + e.size
	if e.allocSize > e.size {
		end = e.off + e.allocSize
	}
	start := e.off + PayloadOffset
	if start > end {
		return
	}
	var b []byte
	switch e.space {
	case spaceNursery:
		if end > uint32(len(c.nursery)) {
			end = uint32(len(c.nursery))
		}
		b = c.nursery[start:end]
	case spaceOld, spaceLarge:
		if end > uint32(len(c.throughput.mem)) {
			end = uint32(len(c.throughput.mem))
		}
		b = c.throughput.mem[start:end]
	case spaceTiny:
		if end > uint32(len(c.tiny.mem)) {
			end = uint32(len(c.tiny.mem))
		}
		b = c.tiny.mem[start:end]
	}
	for i := range b {
		b[i] = 0
	}
}
