//go:build wago_gc_compact_helpers || wago_gc_compact_reads

package gc

import "unsafe"

type compactViewState struct{ compactView *CompactCollectorView }

func (c *Collector) compactReadSnapshot() CompactCollectorView {
	v := CompactCollectorView{Version: CompactNativeABIVersion, Size: CompactCollectorViewSize}
	if c == nil || c.closed || c.direct == nil {
		return v
	}
	x := c.direct.ReadView()
	v.Arena = x.Arena
	v.ArenaBytes = x.ArenaBytes
	v.Starts = x.Starts
	v.StartCount = x.StartCount
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
