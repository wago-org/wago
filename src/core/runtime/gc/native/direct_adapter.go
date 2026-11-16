// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package gc

import (
	"errors"
	"runtime"
)

// NewExperimentalDirectCollector constructs a bounded moving-offset collector
// for the experimental checked-helper runtime bridge. It does NOT implement the
// handle-based NativeView ABI. All live references must be mutable exact roots;
// callers must not retain raw references or payload borrows across collections.
// The two old semispaces and tracing scratch are deliberately explicit prototype
// costs. This constructor does not claim native-code compatibility or performance.
func NewExperimentalDirectCollector(config Config, types []TypeDesc) (*Collector, error) {
	if directHeapCompact || config.ExperimentalCompactOffsets {
		return nil, errors.New("gc: direct layout unavailable for compact configuration/build")
	}
	return newExperimentalOffsetCollector(config, types)
}
func NewExperimentalCompactCollector(config Config, types []TypeDesc) (*Collector, error) {
	if !directHeapCompact || !compactTargetEnabled || config.ExperimentalDirectOffsets {
		return nil, errors.New("gc: compact layout requires exclusive compact helper build/configuration")
	}
	config.ExperimentalCompactOffsets = true
	return newExperimentalOffsetCollector(config, types)
}
func newExperimentalOffsetCollector(config Config, types []TypeDesc) (*Collector, error) {
	if runtime.GOARCH != "amd64" || config.Profile != ProfileThroughput || config.DisableCollection || config.DisableMovingNursery || config.PoisonFreed || config.StressBarriers || config.VerifyAfterCollect {
		return nil, errors.New("gc: unsupported direct-offset experimental configuration")
	}
	if config.NurseryBytes > ^uint32(0)-15 || config.ThroughputHeapBytes > ^uint32(0)-15 {
		return nil, errors.New("gc: direct arena size overflow")
	}
	var err error
	config, err = normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	if err = ValidateTypeDescs(types); err != nil {
		return nil, err
	}
	heap, err := newDirectHeap(align(config.NurseryBytes, 16), align(config.ThroughputHeapBytes, 16), directDescriptors(types))
	if err != nil {
		return nil, err
	}
	c := &Collector{cfg: config, types: append([]TypeDesc(nil), types...), objectAlign: 16, direct: heap}
	for i := range c.types {
		c.types[i].checkScanPlan()
	}
	if err := c.initSubtypeIntervals(); err != nil {
		return nil, err
	}
	return c, nil
}

func directRef(r Ref) directHeapRef { return directHeapRef(r) }
func directDescriptors(types []TypeDesc) []directHeapDescriptor {
	out := make([]directHeapDescriptor, len(types))
	for i, d := range types {
		x := directHeapDescriptor{Kind: directHeapKindStruct, PayloadBytes: d.Size, Align: directDescriptorAlign(d.Align)}
		if d.Kind == KindArray {
			x.Kind = directHeapKindArray
			x.ElementSize = d.ElemSize
			x.ElementRef = isCollectorRefKind(d.Elem)
		}
		for _, f := range d.Fields {
			if isCollectorRefKind(f.Kind) {
				x.RefOffsets = append(x.RefOffsets, f.Offset)
			}
		}
		// Function descriptors occupy canonical type indexes but are never allocated.
		out[i] = x
	}
	return out
}
func (c *Collector) UsesDirectOffsets() bool { return c != nil && c.direct != nil }
func (c *Collector) needsObjectBarrier(r Ref) bool {
	if c.direct != nil {
		return !c.direct.IsYoung(directRef(r))
	}
	sp := c.entry(r).space
	return c.cfg.Profile == ProfileTiny || sp == spaceOld || sp == spaceLarge
}
func (c *Collector) directRemember(r Ref) {
	if c.validObjectRef(r) {
		_ = c.direct.Remember(directRef(r))
	}
}
func (c *Collector) directAlloc(d TypeDesc, aux uint32, roots RootSet) (Ref, error) {
	defer c.refreshDirectView()
	if err := c.errIfClosed(); err != nil {
		return 0, err
	}
	if c.cfg.CollectEveryAlloc {
		if roots == nil {
			return 0, errors.New("gc: moving allocation requires exact roots")
		}
		if err := c.directCollect(roots, c.cfg.ForceMajorEveryMinor); err != nil {
			return 0, err
		}
	}
	allocate := func() (directHeapRef, error) {
		if d.Kind == KindStruct {
			return c.direct.NewStruct(directHeapTypeID(d.ID))
		}
		return c.direct.NewArray(directHeapTypeID(d.ID), aux)
	}
	r, err := allocate()
	if errors.Is(err, errDirectHeapOOM) && roots != nil {
		err = c.directCollect(roots, false)
		if errors.Is(err, errDirectHeapOOM) {
			err = c.directCollect(roots, true)
		}
		if err == nil {
			r, err = allocate()
			if errors.Is(err, errDirectHeapOOM) {
				if err = c.directCollect(roots, true); err == nil {
					r, err = allocate()
				}
			}
		}
	}
	if err != nil {
		return 0, err
	}
	return Ref(r), nil
}

// Snapshot values first and publish rewrites only after collection succeeds.
// This intentionally allocation-using correctness bridge handles aliased root
// slots and mirrors (e.g. runtime global cell plus collector-owned root) without
// observing partially rewritten inputs. Production optimization comes later.
func (c *Collector) directCollect(roots RootSet, full bool) error {
	defer c.refreshDirectView()
	full = full || c.cfg.ForceMajorEveryMinor
	if err := c.errIfClosed(); err != nil {
		return err
	}
	slots := make([]RootSlot, 0, len(c.globalSlots)+len(c.tableSlots))
	for i := range c.globalSlots {
		slots = append(slots, sliceRootSlot{c.globalSlots, i})
	}
	for i := range c.tableSlots {
		slots = append(slots, sliceRootSlot{c.tableSlots, i})
	}
	if roots != nil {
		roots.RangeRoots(func(s RootSlot) bool { slots = append(slots, s); return true })
	}
	if err := c.errIfClosed(); err != nil {
		return err
	}
	values := make([]directHeapRef, len(slots))
	pointers := make([]*directHeapRef, len(slots))
	for i, s := range slots {
		values[i] = directRef(s.GetRef())
		pointers[i] = &values[i]
	}
	var err error
	if full {
		err = c.direct.Full(pointers)
	} else {
		err = c.direct.Minor(pointers)
	}
	if err != nil {
		return err
	}
	for i, s := range slots {
		s.SetRef(Ref(values[i]))
	}
	if full {
		c.stats.FullCollections++
	} else {
		c.stats.MinorCollections++
	}
	return nil
}
