//go:build wago_gc_compact_helpers || (wago_gc_compact_reads || (wago_gc_compact_alloc || ((wago_gc_compact_refstore || wago_gc_compact_shared_checks) || (wago_gc_compact_scalar_arrays || (wago_gc_compact_array_alloc || (wago_gc_compact_fill || (wago_gc_compact_copy || (wago_gc_compact_loop || wago_gc_compact_pair_loop || (wago_gc_compact_range_loop || wago_gc_compact_range_sse2) || wago_gc_compact_range_stride))))))) || wago_gc_compact_refstruct)

package gc

import "unsafe"

type compactViewState struct{ compactView *CompactCollectorView }

func (c *Collector) compactReadSnapshot() CompactCollectorView {
	v := CompactCollectorView{Version: CompactNativeABIVersion, Size: CompactCollectorViewSize}
	if c == nil || c.closed || c.direct == nil {
		return v
	}
	if c.direct.Age2Enabled() {
		v.Policy = CompactPolicyAge2
	}
	x := c.direct.ReadView()
	v.Arena = x.Arena
	v.ArenaBytes = x.ArenaBytes
	v.Starts = x.Starts
	v.StartCount = x.StartCount
	v.Dirty = x.Dirty
	v.DirtyCount = x.DirtyCount
	v.NurseryStart = x.NurseryStart
	v.NurseryBump = x.NurseryBump
	v.OldStart = x.OldStart
	v.OldBump = x.OldBump
	if c.compactNativeAllocationEnabled() {
		w := c.direct.AllocationWords()
		v.NurseryBumpAddress = uintptr(unsafe.Pointer(w.Bump))
		v.NurseryEnd = w.End
		v.AllocationEnabled = 1
	}
	return v
}
func (c *Collector) currentCompactView() *CompactCollectorView {
	if c == nil || !compactTargetEnabled {
		return nil
	}
	return c.compactView
}
func (c *Collector) refreshCompactView() {
	if c != nil && c.compactView != nil {
		*c.compactView = c.compactReadSnapshot()
	}
}
func (c *Collector) CompactView() *CompactCollectorView {
	if c == nil || !compactTargetEnabled || c.closed || c.direct == nil {
		return nil
	}
	if c.compactView == nil {
		c.compactView = &CompactCollectorView{}
	}
	c.refreshCompactView()
	return c.compactView
}

func (c *Collector) compactNativeAllocationEnabled() bool {
	return compactTargetEnabled && nativeCollectorFastPaths && c != nil && !c.closed && c.direct != nil && !failureInjectionEnabled &&
		!c.cfg.CollectEveryAlloc && !c.cfg.VerifyAfterCollect && !c.cfg.PoisonFreed && !c.cfg.StressBarriers && c.cfg.StressNurseryBytes == 0 && !c.cfg.DisableCollection && !c.cfg.DisableMovingNursery && c.cfg.Profile == ProfileThroughput
}
