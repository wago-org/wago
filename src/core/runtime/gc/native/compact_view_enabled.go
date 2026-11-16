//go:build wago_gc_compact_helpers || wago_gc_compact_reads

package gc

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
